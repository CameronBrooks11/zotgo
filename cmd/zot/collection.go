package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:  "collection",
		Usage: "create, rename, and delete collections (local endpoint only)",
		Commands: []*cli.Command{
			collectionCreateCommand(),
			collectionRenameCommand(),
			collectionMoveCommand(),
			collectionDeleteCommand(),
		},
	}
}

func collectionCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "create a collection",
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "parent", Aliases: []string{"p"}, Usage: "parent collection key (omit for a top-level collection)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be created without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: collectionCreateAction,
	}
}

func collectionCreateAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "collection")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	name := cmd.Args().First()
	if name == "" {
		return errors.New("missing collection name (usage: zot collection create <name>)")
	}
	parent := cmd.String("parent")

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	var parentValue any = false
	if parent != "" {
		parentValue = parent
	}
	col, err := json.Marshal(map[string]any{"name": name, "parentCollection": parentValue})
	if err != nil {
		return err
	}
	cols := []json.RawMessage{col}

	w := out(cmd)
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		if parent != "" {
			fmt.Fprintf(w, "  + collection: %s (under %s)\n", name, parent)
		} else {
			fmt.Fprintf(w, "  + collection: %s\n", name)
		}
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitCollectionMutations(w, mode, output.NewLibrary(lib), plannedCollectionCreates(cols))
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Create collection %q in %s?", name, lib.Name)) {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	res, err := c.CreateCollections(ctx, lib, cols)
	if err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		records, resultErr := collectionCreateResults(cols, res)
		if err := emitCollectionMutations(w, mode, output.NewLibrary(lib), records); err != nil {
			return err
		}
		if resultErr != nil {
			return resultErr
		}
	} else {
		reportWriteResult(w, res)
	}
	if !res.Ok() {
		return cli.Exit("", 1)
	}
	return nil
}

// collectionCreateContext reads the name and parent from a create request so a
// planned or failed record can still describe the collection.
func collectionCreateContext(index int, raw json.RawMessage) output.CollectionMutation {
	var context struct {
		Name             string `json:"name"`
		ParentCollection any    `json:"parentCollection"`
	}
	_ = json.Unmarshal(raw, &context)
	record := output.CollectionMutation{
		Index:     index,
		Operation: "create",
		Status:    "planned",
		Name:      context.Name,
	}
	if key, ok := context.ParentCollection.(string); ok {
		record.ParentKey = key
	}
	return record
}

func plannedCollectionCreates(cols []json.RawMessage) []output.CollectionMutation {
	records := make([]output.CollectionMutation, 0, len(cols))
	for i, col := range cols {
		records = append(records, collectionCreateContext(i, col))
	}
	return records
}

// collectionCreateResults maps a batch write response onto one record per
// request, in request order, and reports a structured error if Zotero returned
// an outcome for an index outside the request or more than one for the same one.
func collectionCreateResults(cols []json.RawMessage, res zotero.WriteResult) ([]output.CollectionMutation, error) {
	var problems []string
	problems = append(problems, invalidWriteResultIndices("successful", res.Successful, len(cols))...)
	problems = append(problems, invalidWriteResultIndices("unchanged", res.Unchanged, len(cols))...)
	problems = append(problems, invalidWriteResultIndices("failed", res.Failed, len(cols))...)

	records := make([]output.CollectionMutation, 0, len(cols))
	for index, raw := range cols {
		indexText := strconv.Itoa(index)
		record := collectionCreateContext(index, raw)
		outcomes := 0
		if created, ok := res.Successful[indexText]; ok {
			outcomes++
			col := output.NewCollection(created)
			record.Status = "created"
			record.Key = col.Key
			if col.Name != "" {
				record.Name = col.Name
			}
			if col.ParentKey != "" {
				record.ParentKey = col.ParentKey
			}
		}
		if key, ok := res.Unchanged[indexText]; ok {
			outcomes++
			record.Status = "unchanged"
			record.Key = key
		}
		if failure, ok := res.Failed[indexText]; ok {
			outcomes++
			record.Status = "failed"
			record.Key = failure.Key
			record.Failure = &output.MutationError{Code: failure.Code, Message: failure.Message}
		}
		if outcomes != 1 {
			message := "Zotero returned no outcome for this request"
			if outcomes > 1 {
				message = "Zotero returned multiple outcomes for this request"
			}
			record.Status = "failed"
			record.Key = ""
			record.Failure = &output.MutationError{Message: message}
			problems = append(problems, fmt.Sprintf("request index %d has %d outcomes", index, outcomes))
		}
		records = append(records, record)
	}
	if len(problems) != 0 {
		return records, fmt.Errorf("invalid Zotero write response: %s", strings.Join(problems, "; "))
	}
	return records, nil
}

func collectionRenameCommand() *cli.Command {
	return &cli.Command{
		Name:      "rename",
		Usage:     "rename a collection",
		ArgsUsage: "<key> <new-name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would change without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: collectionRenameAction,
	}
}

func collectionRenameAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "collection")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	key := cmd.Args().Get(0)
	name := cmd.Args().Get(1)
	if key == "" || name == "" {
		return errors.New("usage: zot collection rename <key> <new-name>")
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	col, err := c.Collection(ctx, lib, key)
	if err != nil {
		if errors.Is(err, zotero.ErrNotFound) {
			return fmt.Errorf("no collection with key %q in %s", key, lib.Name)
		}
		return friendly(err)
	}
	data, _ := col.CollectionData()

	record := output.CollectionMutation{Index: 0, Operation: "rename", Status: "planned", Key: key, Name: name}
	w := out(cmd)
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintf(w, "Rename %s: %s → %s\n", key, orDash(data.Name), name)
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitCollectionMutations(w, mode, output.NewLibrary(lib), []output.CollectionMutation{record})
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Rename %s?", key)) {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	if err := c.PatchCollection(ctx, lib, key, patch, col.Version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		record.Status = "renamed"
		return emitCollectionMutations(w, mode, output.NewLibrary(lib), []output.CollectionMutation{record})
	}
	fmt.Fprintf(w, "renamed %s\n", key)
	return nil
}

func collectionMoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "move",
		Usage:     "move a collection under a new parent, or to the top level",
		ArgsUsage: "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "to", Usage: "new parent collection key"},
			&cli.BoolFlag{Name: "to-top", Usage: "move to the top level (no parent)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would change without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: collectionMoveAction,
	}
}

func collectionMoveAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "collection")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	key := cmd.Args().First()
	if key == "" {
		return errors.New("missing collection key (usage: zot collection move <key> --to <parent> | --to-top)")
	}
	to := cmd.String("to")
	toTop := cmd.Bool("to-top")
	switch {
	case to == "" && !toTop:
		return errors.New("specify a destination: --to <parent-key> or --to-top")
	case to != "" && toTop:
		return errors.New("--to and --to-top are mutually exclusive")
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	col, err := c.Collection(ctx, lib, key)
	if err != nil {
		if errors.Is(err, zotero.ErrNotFound) {
			return fmt.Errorf("no collection with key %q in %s", key, lib.Name)
		}
		return friendly(err)
	}
	data, _ := col.CollectionData()

	var parentValue any = false
	dest := "the top level"
	if to != "" {
		parentValue = to
		dest = to
	}

	record := output.CollectionMutation{Index: 0, Operation: "move", Status: "planned", Key: key, Name: data.Name, ParentKey: to}
	w := out(cmd)
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintf(w, "Move %s (%s) → %s\n", key, orDash(data.Name), dest)
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitCollectionMutations(w, mode, output.NewLibrary(lib), []output.CollectionMutation{record})
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Move %s?", key)) {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"parentCollection": parentValue})
	if err != nil {
		return err
	}
	if err := c.PatchCollection(ctx, lib, key, patch, col.Version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		record.Status = "moved"
		return emitCollectionMutations(w, mode, output.NewLibrary(lib), []output.CollectionMutation{record})
	}
	fmt.Fprintf(w, "moved %s\n", key)
	return nil
}

func collectionDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "delete one or more collections by key (their items are kept)",
		ArgsUsage: "<key>...",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be deleted without deleting"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: collectionDeleteAction,
	}
}

func collectionDeleteAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "collection")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	keys := cmd.Args().Slice()
	if len(keys) == 0 {
		return errors.New("missing collection key(s) (usage: zot collection delete <key>...)")
	}
	if len(keys) > zotero.MaxDeleteObjects {
		return fmt.Errorf("%d keys exceeds the %d-collection delete limit", len(keys), zotero.MaxDeleteObjects)
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	w := out(cmd)
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintln(w, "Will delete (their items are kept):")
	}
	found := make([]string, 0, len(keys))
	records := make([]output.CollectionMutation, 0, len(keys))
	for index, key := range keys {
		col, err := c.Collection(ctx, lib, key)
		if errors.Is(err, zotero.ErrNotFound) {
			records = append(records, output.CollectionMutation{Index: index, Operation: "delete", Status: "notFound", Key: key})
			if mode == output.ModeHuman {
				fmt.Fprintf(w, "  ! %s — not found, skipping\n", key)
			}
			continue
		}
		if err != nil {
			return friendly(err)
		}
		data, _ := col.CollectionData()
		found = append(found, key)
		records = append(records, output.CollectionMutation{Index: index, Operation: "delete", Status: "planned", Key: key, Name: data.Name})
		if mode == output.ModeHuman {
			fmt.Fprintf(w, "  - %s: %s\n", key, orDash(data.Name))
		}
	}
	if len(found) == 0 {
		if mode != output.ModeHuman {
			return emitCollectionMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "Nothing to delete.")
		return nil
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitCollectionMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "\nDry run — nothing was deleted.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Delete %d collection(s)? This cannot be undone.", len(found))) {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	version, err := c.LibraryVersion(ctx, lib)
	if err != nil {
		return friendly(err)
	}
	if err := c.DeleteCollections(ctx, lib, found, version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		for i := range records {
			if records[i].Status == "planned" {
				records[i].Status = "deleted"
			}
		}
		return emitCollectionMutations(w, mode, output.NewLibrary(lib), records)
	}
	fmt.Fprintf(w, "deleted %d collection(s)\n", len(found))
	return nil
}

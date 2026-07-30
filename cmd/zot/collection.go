package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:  "collection",
		Usage: "create, rename, and delete collections (local endpoint only)",
		Commands: []*cli.Command{
			collectionCreateCommand(),
			collectionRenameCommand(),
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

	w := out(cmd)
	fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
	if parent != "" {
		fmt.Fprintf(w, "  + collection: %s (under %s)\n", name, parent)
	} else {
		fmt.Fprintf(w, "  + collection: %s\n", name)
	}

	if cmd.Bool("dry-run") {
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Create collection %q in %s?", name, lib.Name)) {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	res, err := c.CreateCollections(ctx, lib, []json.RawMessage{col})
	if err != nil {
		return writeFriendly(err)
	}
	reportWriteResult(w, res)
	if !res.Ok() {
		return cli.Exit("", 1)
	}
	return nil
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

	w := out(cmd)
	fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
	fmt.Fprintf(w, "Rename %s: %s → %s\n", key, orDash(data.Name), name)

	if cmd.Bool("dry-run") {
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Rename %s?", key)) {
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
	patch, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	if err := c.PatchCollection(ctx, lib, key, patch, version); err != nil {
		return writeFriendly(err)
	}
	fmt.Fprintf(w, "renamed %s\n", key)
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
	fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
	fmt.Fprintln(w, "Will delete (their items are kept):")
	var found []string
	for _, key := range keys {
		col, err := c.Collection(ctx, lib, key)
		if errors.Is(err, zotero.ErrNotFound) {
			fmt.Fprintf(w, "  ! %s — not found, skipping\n", key)
			continue
		}
		if err != nil {
			return friendly(err)
		}
		data, _ := col.CollectionData()
		found = append(found, key)
		fmt.Fprintf(w, "  - %s: %s\n", key, orDash(data.Name))
	}
	if len(found) == 0 {
		fmt.Fprintln(w, "Nothing to delete.")
		return nil
	}

	if cmd.Bool("dry-run") {
		fmt.Fprintln(w, "\nDry run — nothing was deleted.")
		return nil
	}
	if !cmd.Bool("yes") && !confirm(os.Stdin, w, fmt.Sprintf("Delete %d collection(s)? This cannot be undone.", len(found))) {
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
	fmt.Fprintf(w, "deleted %d collection(s)\n", len(found))
	return nil
}

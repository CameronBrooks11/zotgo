package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func itemCommand() *cli.Command {
	return &cli.Command{
		Name:  "item",
		Usage: "create and modify items (local endpoint only)",
		Commands: []*cli.Command{
			itemCreateCommand(),
			itemPatchCommand(),
			itemDeleteCommand(),
			itemTemplateCommand(),
		},
	}
}

func itemCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "create items from JSON (a single object or an array) on stdin or --file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "read item JSON from this file instead of stdin"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be created without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: itemCreateAction,
	}
}

func itemCreateAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := itemMutationMode(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}

	file := cmd.String("file")
	fromStdin := file == ""
	raw, err := readItemInput(file)
	if err != nil {
		return err
	}
	items, err := parseItemsInput(raw)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("no items to create")
	}
	if len(items) > zotero.MaxWriteObjects {
		return fmt.Errorf("%d items exceeds the %d-item batch limit", len(items), zotero.MaxWriteObjects)
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
		printItemSummary(w, items)
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitItemMutations(w, mode, output.NewLibrary(lib), plannedItemCreates(items))
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}

	if mode == output.ModeHuman && !cmd.Bool("yes") {
		if fromStdin {
			return errors.New("item JSON was read from stdin, so I cannot prompt — re-run with --yes (or --dry-run), or use --file")
		}
		if !confirm(os.Stdin, w, fmt.Sprintf("Create %d item(s) in %s?", len(items), lib.Name)) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}

	res, err := c.CreateItems(ctx, lib, items)
	if err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		records, resultErr := itemCreateResults(items, res)
		if err := emitItemMutations(w, mode, output.NewLibrary(lib), records); err != nil {
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

// readItemInput reads the item JSON from a file, or from stdin when file is "".
func readItemInput(file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	return io.ReadAll(os.Stdin)
}

// parseItemsInput accepts either a single item object or an array of them and
// normalizes to a slice of item objects.
func parseItemsInput(raw []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("no item JSON provided")
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, fmt.Errorf("invalid item JSON array: %w", err)
		}
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("invalid item JSON: %w", err)
	}
	return []json.RawMessage{json.RawMessage(trimmed)}, nil
}

// printItemSummary lists what will be created, so the target is never a mystery.
func printItemSummary(w io.Writer, items []json.RawMessage) {
	for _, it := range items {
		var m struct {
			ItemType string `json:"itemType"`
			Title    string `json:"title"`
		}
		_ = json.Unmarshal(it, &m)
		fmt.Fprintf(w, "  + %s: %s\n", orDash(m.ItemType), orDash(m.Title))
	}
}

func itemMutationMode(cmd *cli.Command) (output.Mode, error) {
	mode, err := outputMode(cmd)
	if err != nil {
		return mode, err
	}
	if mode == output.ModeRaw {
		return mode, output.ErrRawUnavailable
	}
	if mode != output.ModeHuman && !cmd.Bool("dry-run") && !cmd.Bool("yes") {
		return mode, fmt.Errorf("%s item writes require --yes (or --dry-run)", mode)
	}
	return mode, nil
}

func plannedItemCreates(items []json.RawMessage) []output.ItemMutation {
	records := make([]output.ItemMutation, 0, len(items))
	for i, item := range items {
		records = append(records, itemCreateContext(i, item))
	}
	return records
}

func itemCreateContext(index int, item json.RawMessage) output.ItemMutation {
	var context struct {
		Type  string `json:"itemType"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(item, &context)
	return output.ItemMutation{
		Index:     index,
		Operation: "create",
		Status:    "planned",
		Type:      context.Type,
		Title:     context.Title,
	}
}

func itemCreateResults(items []json.RawMessage, res zotero.WriteResult) ([]output.ItemMutation, error) {
	var problems []string
	problems = append(problems, invalidWriteResultIndices("successful", res.Successful, len(items))...)
	problems = append(problems, invalidWriteResultIndices("unchanged", res.Unchanged, len(items))...)
	problems = append(problems, invalidWriteResultIndices("failed", res.Failed, len(items))...)

	records := make([]output.ItemMutation, 0, len(items))
	for index, raw := range items {
		indexText := strconv.Itoa(index)
		record := itemCreateContext(index, raw)
		outcomes := 0
		if created, ok := res.Successful[indexText]; ok {
			outcomes++
			item := output.NewItem(created)
			record.Status = "created"
			record.Key = item.Key
			if item.Type != "" {
				record.Type = item.Type
			}
			if item.Title != "" {
				record.Title = item.Title
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
			record.Failure = &output.ItemMutationError{Code: failure.Code, Message: failure.Message}
		}
		if outcomes != 1 {
			message := "Zotero returned no outcome for this request"
			if outcomes > 1 {
				message = "Zotero returned multiple outcomes for this request"
			}
			record.Status = "failed"
			record.Key = ""
			record.Failure = &output.ItemMutationError{Message: message}
			problems = append(problems, fmt.Sprintf("request index %d has %d outcomes", index, outcomes))
		}
		records = append(records, record)
	}
	if len(problems) != 0 {
		return records, fmt.Errorf("invalid Zotero write response: %s", strings.Join(problems, "; "))
	}
	return records, nil
}

func invalidWriteResultIndices[T any](category string, entries map[string]T, requestCount int) []string {
	var problems []string
	for indexText := range entries {
		index, err := strconv.Atoi(indexText)
		if err != nil || index < 0 || index >= requestCount {
			problems = append(problems, fmt.Sprintf("%s index %q is outside the request", category, indexText))
		}
	}
	return problems
}

func reportWriteResult(w io.Writer, res zotero.WriteResult) {
	for _, i := range sortedIndices(res.Successful) {
		fmt.Fprintf(w, "created %s\n", res.Successful[i].Key)
	}
	for _, i := range sortedIndices(res.Unchanged) {
		fmt.Fprintf(w, "unchanged %s\n", res.Unchanged[i])
	}
	for _, i := range sortedIndices(res.Failed) {
		f := res.Failed[i]
		fmt.Fprintf(w, "failed [%s] %s: %s (code %d)\n", i, f.Key, f.Message, f.Code)
	}
}

// sortedIndices returns the map's string-integer keys in numeric order, so the
// report follows request order rather than Go's random map iteration.
func sortedIndices[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	return keys
}

// ensureLocalKey makes sure the client can write: it reuses a persisted key, or
// prompts the user in Zotero and persists the key if they chose to remember it.
func ensureLocalKey(ctx context.Context, c *zotero.Client) error {
	if c.HasLocalKey() {
		return nil
	}
	fmt.Fprintln(os.Stderr, "Authorizing with Zotero — approve the prompt in the app…")
	remember, err := c.Authorize(ctx, "zotgo")
	if err != nil {
		return writeFriendly(err)
	}
	if remember {
		if err := saveLocalKey(c.LocalKey()); err != nil {
			fmt.Fprintf(os.Stderr, "zot: warning: could not save the local key: %v\n", err)
		}
	}
	return nil
}

// confirm asks a yes/no question, defaulting to no.
func confirm(in io.Reader, w io.Writer, question string) bool {
	fmt.Fprintf(w, "%s [y/N] ", question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// writeFriendly turns the write-path sentinels into actionable messages.
func writeFriendly(err error) error {
	switch {
	case errors.Is(err, zotero.ErrAuthorizeDenied):
		return errors.New("authorization denied in Zotero")
	case errors.Is(err, zotero.ErrWriteUnauthorized):
		_ = clearLocalKey() // a stale/consumed key is worse than none
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero rejected the local key (it may have expired) — re-run to re-authorize")
	case errors.Is(err, zotero.ErrPreconditionRequired):
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero requires a write precondition zotgo did not supply (this is a bug)")
	case errors.Is(err, zotero.ErrPreconditionFailed):
		return errors.New("the library changed since this write was prepared — re-run")
	}
	return friendly(err)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func itemTemplateCommand() *cli.Command {
	return &cli.Command{
		Name:      "template",
		Usage:     "print a blank item skeleton for an item type, to fill in and pipe to `item create`",
		ArgsUsage: "<item-type>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			itemType := cmd.Args().First()
			if itemType == "" {
				return errors.New("missing item type (e.g. zot item template book)")
			}
			if cmd.Bool("web") {
				return errors.New("item template uses the local endpoint")
			}
			c, err := newClient(cmd)
			if err != nil {
				return err
			}
			tpl, err := c.ItemTemplate(ctx, itemType)
			if err != nil {
				return friendly(err)
			}
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, tpl, "", "  "); err != nil {
				return err
			}
			fmt.Fprintln(out(cmd), pretty.String())
			return nil
		},
	}
}

func itemPatchCommand() *cli.Command {
	return &cli.Command{
		Name:      "patch",
		Usage:     "update fields of one item from a JSON object on stdin or --file",
		ArgsUsage: "<item-key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "read the patch JSON from this file instead of stdin"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would change without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: itemPatchAction,
	}
}

func itemPatchAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := itemMutationMode(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	key := cmd.Args().First()
	if key == "" {
		return errors.New("missing item key (usage: zot item patch <item-key>)")
	}
	file := cmd.String("file")
	fromStdin := file == ""
	raw, err := readItemInput(file)
	if err != nil {
		return err
	}
	patch, err := parsePatchInput(raw)
	if err != nil {
		return err
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	item, err := c.Item(ctx, lib, key)
	if err != nil {
		if errors.Is(err, zotero.ErrNotFound) {
			return fmt.Errorf("no item with key %q in %s", key, lib.Name)
		}
		return friendly(err)
	}

	w := out(cmd)
	fields := patchFields(patch)
	record := output.ItemMutation{
		Index:     0,
		Operation: "patch",
		Status:    "planned",
		Key:       key,
		Type:      item.ItemType(),
		Title:     item.Title(),
		Fields:    fields,
	}
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintf(w, "Patching %s (%s): %s\n", key, item.ItemType(), orDash(item.Title()))
		fmt.Fprintf(w, "  fields: %s\n", strings.Join(fields, ", "))
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitItemMutations(w, mode, output.NewLibrary(lib), []output.ItemMutation{record})
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") {
		if fromStdin {
			return errors.New("the patch was read from stdin, so I cannot prompt — re-run with --yes (or --dry-run), or use --file")
		}
		if !confirm(os.Stdin, w, fmt.Sprintf("Patch %s?", key)) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	version, err := c.LibraryVersion(ctx, lib)
	if err != nil {
		return friendly(err)
	}
	if err := c.PatchItem(ctx, lib, key, patch, version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		record.Status = "patched"
		return emitItemMutations(w, mode, output.NewLibrary(lib), []output.ItemMutation{record})
	}
	fmt.Fprintf(w, "patched %s\n", key)
	return nil
}

func itemDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "delete one or more items by key (destructive)",
		ArgsUsage: "<item-key>...",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be deleted without deleting"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: itemDeleteAction,
	}
}

func itemDeleteAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := itemMutationMode(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	keys := cmd.Args().Slice()
	if len(keys) == 0 {
		return errors.New("missing item key(s) (usage: zot item delete <item-key>...)")
	}
	if len(keys) > zotero.MaxDeleteObjects {
		return fmt.Errorf("%d keys exceeds the %d-item delete limit", len(keys), zotero.MaxDeleteObjects)
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
		fmt.Fprintln(w, "Will delete:")
	}
	found := make([]string, 0, len(keys))
	records := make([]output.ItemMutation, 0, len(keys))
	for index, key := range keys {
		item, err := c.Item(ctx, lib, key)
		if errors.Is(err, zotero.ErrNotFound) {
			records = append(records, output.ItemMutation{Index: index, Operation: "delete", Status: "notFound", Key: key})
			if mode == output.ModeHuman {
				fmt.Fprintf(w, "  ! %s — not found, skipping\n", key)
			}
			continue
		}
		if err != nil {
			return friendly(err)
		}
		found = append(found, key)
		records = append(records, output.ItemMutation{
			Index:     index,
			Operation: "delete",
			Status:    "planned",
			Key:       key,
			Type:      item.ItemType(),
			Title:     item.Title(),
		})
		if mode == output.ModeHuman {
			fmt.Fprintf(w, "  - %s (%s): %s\n", key, item.ItemType(), orDash(item.Title()))
		}
	}
	if len(found) == 0 {
		if mode != output.ModeHuman {
			return emitItemMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "Nothing to delete.")
		return nil
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitItemMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "\nDry run — nothing was deleted.")
		return nil
	}
	if mode == output.ModeHuman && !cmd.Bool("yes") {
		if !confirm(os.Stdin, w, fmt.Sprintf("Delete %d item(s)? This cannot be undone.", len(found))) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}
	version, err := c.LibraryVersion(ctx, lib)
	if err != nil {
		return friendly(err)
	}
	if err := c.DeleteItems(ctx, lib, found, version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		for i := range records {
			if records[i].Status == "planned" {
				records[i].Status = "deleted"
			}
		}
		return emitItemMutations(w, mode, output.NewLibrary(lib), records)
	}
	fmt.Fprintf(w, "deleted %d item(s)\n", len(found))
	return nil
}

// parsePatchInput requires a single JSON object of fields to change.
func parsePatchInput(raw []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("no patch JSON provided")
	}
	if trimmed[0] != '{' {
		return nil, errors.New("a patch must be a JSON object of fields to change")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return nil, fmt.Errorf("invalid patch JSON: %w", err)
	}
	if len(m) == 0 {
		return nil, errors.New("the patch is empty")
	}
	return json.RawMessage(trimmed), nil
}

// patchFields lists the field names a patch will set, in a stable order.
func patchFields(patch json.RawMessage) []string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(patch, &m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

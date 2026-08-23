package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func tagCommand() *cli.Command {
	return &cli.Command{
		Name:  "tag",
		Usage: "add, remove, and purge tags (local endpoint only)",
		Description: "Add or remove tags on one item, or purge tags from every item in the selected library. " +
			"Write commands support --dry-run and require confirmation unless --yes is set.",
		Commands: []*cli.Command{
			tagAddCommand(),
			tagRemoveCommand(),
			tagPurgeCommand(),
		},
	}
}

func tagPurgeCommand() *cli.Command {
	return &cli.Command{
		Name:      "purge",
		Aliases:   []string{"delete"},
		Usage:     "remove tag(s) from EVERY item in the library",
		ArgsUsage: "<tag>...",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be removed without deleting"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: tagPurgeAction,
	}
}

func tagPurgeAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := machineWriteMode(cmd, "tag")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return errors.New("missing tag name(s) (usage: zot tag purge <tag>...)")
	}
	if len(names) > zotero.MaxDeleteObjects {
		return fmt.Errorf("%d tags exceeds the %d-tag purge limit", len(names), zotero.MaxDeleteObjects)
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}

	w := out(cmd)
	records := make([]output.TagMutation, 0, len(names))
	for i, n := range names {
		records = append(records, output.TagMutation{Index: i, Operation: "delete", Status: "planned", Tag: n})
	}
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintln(w, "Will remove these tags from EVERY item:")
		for _, n := range names {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitTagMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "\nDry run — nothing was deleted.")
		return nil
	}
	if proceed, err := confirmWrite(ctx, cmd, c, mode, w, fmt.Sprintf("Remove %d tag(s) from all items? This cannot be undone.", len(names))); err != nil {
		return err
	} else if !proceed {
		return nil
	}

	if err := ensureWriteAuthority(ctx, cmd, c, mode); err != nil {
		return err
	}
	version, err := c.LibraryVersion(ctx, lib)
	if err != nil {
		return friendly(err)
	}
	if err := c.DeleteTags(ctx, zotero.OpTagDelete, lib, names, version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		for i := range records {
			records[i].Status = "deleted"
		}
		return emitTagMutations(w, mode, output.NewLibrary(lib), records)
	}
	fmt.Fprintf(w, "removed %d tag(s)\n", len(names))
	return nil
}

func tagAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "add tag(s) to one item",
		ArgsUsage: "<item-key> <tag>...",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "item", Aliases: []string{"i"}, Usage: "item key to tag (deprecated alias for the first argument)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would change without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error { return itemTagAction(ctx, cmd, true) },
	}
}

func tagRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Usage:     "remove tag(s) from one item",
		ArgsUsage: "<item-key> <tag>...",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "item", Aliases: []string{"i"}, Usage: "item key to untag (deprecated alias for the first argument)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would change without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error { return itemTagAction(ctx, cmd, false) },
	}
}

// itemTagAction adds or removes tags on a single item by reading its tags,
// splicing them, and patching the whole array back (the item write replaces it).
func itemTagAction(ctx context.Context, cmd *cli.Command, add bool) error {
	mode, err := machineWriteMode(cmd, "tag")
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}
	operation := "remove"
	if add {
		operation = "add"
	}
	// The item key is the first positional argument; --item is the deprecated
	// alias and still wins when set, so the old `--item KEY tag...` form is
	// unchanged and the new `KEY tag...` form needs no flag.
	itemKey := cmd.String("item")
	names := cmd.Args().Slice()
	if itemKey == "" {
		if len(names) == 0 {
			return fmt.Errorf("missing item key (usage: zot tag %s <item-key> <tag>...)", operation)
		}
		itemKey = names[0]
		names = names[1:]
	}
	if len(names) == 0 {
		return fmt.Errorf("missing tag name(s) (usage: zot tag %s <item-key> <tag>...)", operation)
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}

	item, err := c.Item(ctx, lib, itemKey)
	if err != nil {
		if errors.Is(err, zotero.ErrNotFound) {
			return fmt.Errorf("no item with key %q in %s", itemKey, lib.Name)
		}
		return friendly(err)
	}
	// A tag write is a full-array replace, so building it from a silently-empty
	// tag set would wipe every existing tag. Never let a decode error through.
	data, err := item.ItemData()
	if err != nil {
		return fmt.Errorf("inspect item %q before tagging: %w", itemKey, err)
	}

	var next []zotero.Tag
	var changed []string
	verb, past := "add", "added"
	tagOp := zotero.OpTagAdd
	if add {
		next, changed = addTags(data.Tags, names)
	} else {
		next, changed = removeTags(data.Tags, names)
		verb, past = "remove", "removed"
		tagOp = zotero.OpTagRemove
	}

	// One record per requested tag, in order: the ones actually spliced are
	// "planned" (and become past-tense after the write), the rest "unchanged".
	// changed lists each affected tag once, so a name repeated on the command
	// line is credited to its first occurrence and duplicates read "unchanged" —
	// the splice only adds or removes it once.
	remaining := make(map[string]int, len(changed))
	for _, n := range changed {
		remaining[n]++
	}
	records := make([]output.TagMutation, 0, len(names))
	for i, n := range names {
		status := "unchanged"
		if remaining[n] > 0 {
			status = "planned"
			remaining[n]--
		}
		records = append(records, output.TagMutation{Index: i, Operation: verb, Status: status, Tag: n, Item: itemKey})
	}

	w := out(cmd)
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
		fmt.Fprintf(w, "%s (%s): %s\n", itemKey, item.ItemType(), orDash(item.Title()))
	}
	if len(changed) == 0 {
		if mode != output.ModeHuman {
			return emitTagMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintf(w, "No change — nothing to %s.\n", verb)
		return nil
	}
	if mode == output.ModeHuman {
		fmt.Fprintf(w, "  %s tags: %s\n", verb, strings.Join(changed, ", "))
	}

	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitTagMutations(w, mode, output.NewLibrary(lib), records)
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}
	if proceed, err := confirmWrite(ctx, cmd, c, mode, w, fmt.Sprintf("%s %d tag(s) on %s?", capitalize(verb), len(changed), itemKey)); err != nil {
		return err
	} else if !proceed {
		return nil
	}

	if err := ensureWriteAuthority(ctx, cmd, c, mode); err != nil {
		return err
	}
	patch, err := tagsPatch(next)
	if err != nil {
		return err
	}
	if err := c.PatchItem(ctx, tagOp, lib, itemKey, patch, item.Version); err != nil {
		return writeFriendly(err)
	}
	if mode != output.ModeHuman {
		for i := range records {
			if records[i].Status == "planned" {
				records[i].Status = past
			}
		}
		return emitTagMutations(w, mode, output.NewLibrary(lib), records)
	}
	fmt.Fprintf(w, "updated tags on %s\n", itemKey)
	return nil
}

// addTags returns existing plus any of names not already present, and which were
// actually added.
func addTags(existing []zotero.Tag, names []string) (next []zotero.Tag, added []string) {
	have := make(map[string]bool, len(existing))
	for _, t := range existing {
		have[t.Tag] = true
	}
	next = append(next, existing...)
	for _, n := range names {
		if !have[n] {
			next = append(next, zotero.Tag{Tag: n})
			have[n] = true
			added = append(added, n)
		}
	}
	return next, added
}

// removeTags returns existing minus any of names, and which were actually removed.
func removeTags(existing []zotero.Tag, names []string) (next []zotero.Tag, removed []string) {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	for _, t := range existing {
		if drop[t.Tag] {
			removed = append(removed, t.Tag)
			continue
		}
		next = append(next, t)
	}
	return next, removed
}

// capitalize upper-cases the first letter of a short ASCII word.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// tagsPatch builds the item patch that replaces the tags array.
func tagsPatch(tags []zotero.Tag) (json.RawMessage, error) {
	if tags == nil {
		tags = []zotero.Tag{}
	}
	arr, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(`{"tags":` + string(arr) + `}`), nil
}

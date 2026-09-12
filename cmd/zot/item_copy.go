package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
	cli "github.com/urfave/cli/v3"
)

func itemCopyCommand() *cli.Command {
	return &cli.Command{
		Name:      "copy",
		Usage:     "copy items into another library, with their notes, files and annotations",
		ArgsUsage: "<item-key>...",
		Description: "Copies whole items between libraries: the item, its notes and attachments, the\n" +
			"bytes of any managed file, and the annotations on those attachments. Local\n" +
			"endpoint only — carrying a file means reading it from Zotero's own storage.\n\n" +
			"--from and --to are both required. A lease covers the whole command as\n" +
			"`item.copy`.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "from", Usage: "source library selector ('me', a group name, or a group id)"},
			&cli.StringFlag{Name: "to", Usage: "target library selector ('me', a group name, or a group id)"},
			&cli.StringFlag{Name: "collection", Usage: "put the copies in this collection of the target library"},
			&cli.BoolFlag{Name: "no-attachments", Usage: "copy metadata and notes only, leaving files and annotations behind"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be copied without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: itemCopyAction,
	}
}

func itemCopyAction(ctx context.Context, cmd *cli.Command) error {
	mode, err := itemMutationMode(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}

	keys := cmd.Args().Slice()
	if len(keys) == 0 {
		return errors.New("missing item key (usage: zot item copy --from <lib> --to <lib> <item-key>...)")
	}
	// Neither endpoint is inferred. A copy has two of them, and the lesson of the
	// grant default is that a write target guessed from context is a write in the
	// wrong place.
	fromSelector, toSelector := strings.TrimSpace(cmd.String("from")), strings.TrimSpace(cmd.String("to"))
	if fromSelector == "" || toSelector == "" {
		return errors.New("zot item copy requires both --from and --to: a copy has two libraries and neither is inferred")
	}

	c, err := newClient(cmd)
	if err != nil {
		return err
	}
	source, err := c.ResolveLibrary(ctx, fromSelector)
	if err != nil {
		return friendly(err)
	}
	target, err := c.ResolveLibrary(ctx, toSelector)
	if err != nil {
		return friendly(err)
	}
	if source.Kind == target.Kind && source.ID == target.ID {
		return fmt.Errorf("--from and --to are the same library (%s); a copy needs two", source.Name)
	}

	withAttachments := !cmd.Bool("no-attachments")
	if withAttachments {
		if err := requireTargetAcceptsFiles(ctx, c, target); err != nil {
			return err
		}
	}

	plan := copyPlan{}
	for _, key := range keys {
		planned, err := planItemCopy(ctx, c, source, key, withAttachments)
		if err != nil {
			return err
		}
		plan.items = append(plan.items, planned)
	}

	var collections []string
	if collection := strings.TrimSpace(cmd.String("collection")); collection != "" {
		collections = []string{collection}
	}

	w := out(cmd)
	if mode == output.ModeHuman {
		printCopyPlan(w, source, target, cmd.String("collection"), plan)
	}
	if cmd.Bool("dry-run") {
		if mode != output.ModeHuman {
			return emitItemCopies(w, mode, output.NewLibrary(target), plannedCopies(plan))
		}
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}

	counts := plan.counts()
	prompt := fmt.Sprintf("Copy %d item(s) and %d child object(s) into %s?",
		counts.items, counts.notes+counts.attachments+counts.annotations, target.Name)
	if proceed, err := confirmWrite(ctx, cmd, c, mode, w, prompt); err != nil {
		return err
	} else if !proceed {
		return nil
	}
	if err := ensureWriteAuthority(ctx, cmd, c, mode); err != nil {
		return err
	}

	records := make([]output.ItemCopy, 0, len(plan.items))
	for i, planned := range plan.items {
		records = append(records, executeItemCopy(ctx, c, source, target, i, planned, collections))
	}

	if mode != output.ModeHuman {
		return emitItemCopies(w, mode, output.NewLibrary(target), records)
	}
	printCopyResults(w, records)
	for _, record := range records {
		if record.Status == output.StatusFailed || record.Status == output.StatusPartial {
			return cli.Exit("", 1)
		}
	}
	return nil
}

// requireTargetAcceptsFiles fails before anything is written when the target
// library cannot take files. Zotero reports this per library and doctor already
// shows it, but no write path has ever gated on it — so the failure would
// otherwise arrive halfway through a tree, with items already created.
func requireTargetAcceptsFiles(ctx context.Context, c *zotero.Client, target zotero.LibraryRef) error {
	libraries, err := c.LibraryFileAccess(ctx)
	if err != nil {
		// The capability probe is advisory. Refusing the copy because we could not
		// ask would be worse than attempting it and reporting a real failure.
		return nil
	}
	// Matched by name, deliberately. LibraryFiles.ID is Zotero's own numeric
	// library id — My Library is 1 there — while LibraryRef.ID is the 0 sentinel
	// the Local API routes on. Comparing the two would be the exact fail-open that
	// Q6 of docs/design/write-authority.md names as the top correctness risk:
	// deriving identity from a Zotero response instead of the canonical token.
	// Do not "fix" this into an id comparison without resolving that first.
	for _, library := range libraries {
		if library.Name == target.Name && !library.FilesEditable {
			return fmt.Errorf("%s does not accept file attachments, so a copy cannot carry them; re-run with --no-attachments to copy metadata and notes only", target.Name)
		}
	}
	return nil
}

func printCopyPlan(w io.Writer, source, target zotero.LibraryRef, collection string, plan copyPlan) {
	counts := plan.counts()
	fmt.Fprintf(w, "From: %s\n", source.Name)
	fmt.Fprintf(w, "To:   %s", target.Name)
	if collection != "" {
		fmt.Fprintf(w, " — collection %s", collection)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Carrying: %d item(s), %d note(s), %d attachment(s) (%d with files), %d annotation(s)\n",
		counts.items, counts.notes, counts.attachments, counts.files, counts.annotations)
	if counts.skipped > 0 {
		fmt.Fprintf(w, "Skipping: %d child object(s)\n", counts.skipped)
	}
	for _, item := range plan.items {
		fmt.Fprintf(w, "  + %s: %s\n", orDash(item.itemType), orDash(item.title))
		for _, child := range item.children {
			mark := "+"
			if child.skipReason != "" {
				mark = "-"
			}
			fmt.Fprintf(w, "    %s %s: %s", mark, child.kind, orDash(child.title))
			if child.skipReason != "" {
				fmt.Fprintf(w, " (%s)", child.skipReason)
			}
			fmt.Fprintln(w)
			for _, annotation := range child.children {
				fmt.Fprintf(w, "      + annotation: %s\n", orDash(annotation.title))
			}
		}
	}
}

func printCopyResults(w io.Writer, records []output.ItemCopy) {
	for _, record := range records {
		fmt.Fprintf(w, "%s %s → %s\n", record.Status, record.SourceKey, orDash(record.Key))
		for _, child := range record.Children {
			fmt.Fprintf(w, "  %s %s %s", child.Status, child.Kind, orDash(child.Key))
			if child.Reason != "" {
				fmt.Fprintf(w, " (%s)", child.Reason)
			}
			if child.File != nil {
				fmt.Fprintf(w, " [file %s]", child.File.Status)
			}
			fmt.Fprintln(w)
			for _, annotation := range child.Children {
				fmt.Fprintf(w, "    %s annotation %s\n", annotation.Status, orDash(annotation.Key))
			}
		}
	}
}

// plannedCopies renders a plan as dry-run records, so --dry-run and a real run
// speak the same shape and a script can diff one against the other.
func plannedCopies(plan copyPlan) []output.ItemCopy {
	records := make([]output.ItemCopy, 0, len(plan.items))
	for i, item := range plan.items {
		record := output.ItemCopy{
			Index: i, Operation: output.OpCopy, Status: output.StatusPlanned,
			SourceKey: item.sourceKey, Type: item.itemType, Title: item.title,
			Children: []output.CopiedChild{},
		}
		for j, child := range item.children {
			childRecord := output.CopiedChild{
				Index: j, Kind: child.kind, SourceKey: child.sourceKey,
				Title: child.title, Status: output.StatusPlanned, Reason: child.skipReason,
			}
			if child.skipReason != "" && !child.managed {
				childRecord.Status = output.StatusSkipped
			}
			for k, annotation := range child.children {
				childRecord.Children = append(childRecord.Children, output.CopiedChild{
					Index: k, Kind: "annotation", SourceKey: annotation.sourceKey,
					Title: annotation.title, Status: output.StatusPlanned,
				})
			}
			record.Children = append(record.Children, childRecord)
		}
		records = append(records, record)
	}
	return records
}

func emitItemCopies(w io.Writer, mode output.Mode, lib *output.Library, records []output.ItemCopy) error {
	return emitMutations(w, mode, output.KindItemCopies, output.KindItemCopy, lib, records)
}

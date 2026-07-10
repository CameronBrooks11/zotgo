package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/render"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func exportCommand() *cli.Command {
	return &cli.Command{
		Name:      "export",
		Usage:     "export items via a Zotero translator or a zotgo summary",
		ArgsUsage: "<format>",
		Description: "Zotero translators: " + strings.Join(zotero.ExportFormats(), ", ") + " (alias: bib = bibtex).\n" +
			"zotgo summaries: json, summary-csv, summary-md (aliases: md, markdown = summary-md).\n\n" +
			"mods, tei, and the rdf_* formats wrap each page in one XML root element,\n" +
			"so they export only results that fit in a single page; narrow the query\n" +
			"with --collection or --tag.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "limit to a collection (key or exact name)"},
			&cli.StringSliceFlag{Name: "tag", Aliases: []string{"t"}, Usage: "filter by tag (repeatable; AND)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "write to a file instead of stdout"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format := strings.ToLower(cmd.Args().First())
			if format == "" {
				return fmt.Errorf("missing format (usage: zot export <bibtex|csljson|json|csv|md>)")
			}

			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}

			// Export operates on top-level items across the whole selection.
			opts := zotero.ItemsOptions{Top: true, Tags: cmd.StringSlice("tag")}
			if sel := cmd.String("collection"); sel != "" {
				key, err := resolveCollectionKey(ctx, c, lib, sel)
				if err != nil {
					return err
				}
				opts.Collection = key
			}

			data, err := renderExport(ctx, c, lib, opts, format)
			if err != nil {
				return err
			}
			return writeExport(cmd, cmd.String("output"), data)
		},
	}
}

// localFormats are shaped by zotgo rather than by a Zotero translator. They are
// named apart from the translators so that `csv` unambiguously means Zotero's
// own CSV, not zotgo's summary table.
var localFormats = map[string]func(*strings.Builder, []zotero.Envelope) error{
	"json":        func(b *strings.Builder, items []zotero.Envelope) error { return render.JSON(b, items) },
	"summary-csv": func(b *strings.Builder, items []zotero.Envelope) error { return render.CSV(b, items) },
	"summary-md":  func(b *strings.Builder, items []zotero.Envelope) error { return render.Markdown(b, items) },
}

// formatAliases spell shorthands used on the command line.
var formatAliases = map[string]string{
	"bib":      "bibtex",
	"markdown": "summary-md",
	"md":       "summary-md",
}

// renderExport produces the export bytes for a format: Zotero's translators
// handle their own, and zotgo shapes the rest from item envelopes.
func renderExport(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, opts zotero.ItemsOptions, format string) ([]byte, error) {
	if canonical, ok := formatAliases[format]; ok {
		format = canonical
	}

	if shape, ok := localFormats[format]; ok {
		items, err := c.AllItems(ctx, lib, opts)
		if err != nil {
			return nil, friendly(err)
		}
		var buf strings.Builder
		if err := shape(&buf, items); err != nil {
			return nil, err
		}
		return []byte(buf.String()), nil
	}

	data, err := c.Export(ctx, lib, opts, format)
	if errors.Is(err, zotero.ErrUnsupportedFormat) {
		return nil, fmt.Errorf("unknown format %q (want %s)", format, strings.Join(allFormats(), ", "))
	}
	return data, friendly(err)
}

// allFormats lists every format the export command accepts, sorted.
func allFormats() []string {
	names := zotero.ExportFormats()
	for name := range localFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func writeExport(cmd *cli.Command, path string, data []byte) error {
	if path == "" {
		_, err := out(cmd).Write(data)
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	fmt.Fprintf(errOut(cmd), "wrote %d bytes to %s\n", len(data), path)
	return nil
}

// writeFileAtomic writes data to path via a temp file in the same directory,
// then renames it into place. An export that fails partway — a full disk, a
// canceled request, a crash — leaves the previous file untouched rather than
// truncated, and readers never observe a partial export.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		// No-ops once the rename has succeeded.
		f.Close()
		os.Remove(tmp)
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}
	// Durability before the rename: a rename that wins the race to disk ahead
	// of the data would publish an empty file after a crash.
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// CreateTemp makes 0600; exports are ordinary files.
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// errOut returns the writer for diagnostics that should not pollute stdout
// (e.g. when export output is redirected to a file).
func errOut(cmd *cli.Command) io.Writer {
	if w := cmd.Root().ErrWriter; w != nil {
		return w
	}
	return os.Stderr
}

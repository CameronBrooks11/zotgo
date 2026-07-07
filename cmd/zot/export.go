package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/render"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func exportCommand() *cli.Command {
	return &cli.Command{
		Name:      "export",
		Usage:     "export items as bibtex, csljson, json, csv, or md",
		ArgsUsage: "<format>",
		Description: "Formats: bibtex (aliases: bib), csljson, json, csv, md (aliases: markdown).\n" +
			"bibtex and csljson are produced by Zotero itself; json/csv/md are shaped by zotgo.",
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

// renderExport produces the export bytes for a format, delegating BibTeX and
// CSL-JSON to Zotero and shaping json/csv/md locally.
func renderExport(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, opts zotero.ItemsOptions, format string) ([]byte, error) {
	switch format {
	case "bibtex", "bib":
		data, err := c.ExportBibtex(ctx, lib, opts)
		return data, friendly(err)
	case "csljson":
		data, err := c.ExportCSLJSON(ctx, lib, opts)
		return data, friendly(err)
	case "json", "csv", "md", "markdown":
		items, err := c.AllItems(ctx, lib, opts)
		if err != nil {
			return nil, friendly(err)
		}
		return shapeItems(items, format)
	default:
		return nil, fmt.Errorf("unknown format %q (want bibtex, csljson, json, csv, or md)", format)
	}
}

func shapeItems(items []zotero.Envelope, format string) ([]byte, error) {
	var buf strings.Builder
	var err error
	switch format {
	case "json":
		err = render.JSON(&buf, items)
	case "csv":
		err = render.CSV(&buf, items)
	case "md", "markdown":
		err = render.Markdown(&buf, items)
	}
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func writeExport(cmd *cli.Command, path string, data []byte) error {
	if path == "" {
		_, err := out(cmd).Write(data)
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(errOut(cmd), "wrote %d bytes to %s\n", len(data), path)
	return nil
}

// errOut returns the writer for diagnostics that should not pollute stdout
// (e.g. when export output is redirected to a file).
func errOut(cmd *cli.Command) io.Writer {
	if w := cmd.Root().ErrWriter; w != nil {
		return w
	}
	return os.Stderr
}

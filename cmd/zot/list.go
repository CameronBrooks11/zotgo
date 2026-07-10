package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/render"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func listCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list items in a library or collection",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "limit to a collection (key or exact name)"},
			&cli.StringSliceFlag{Name: "tag", Aliases: []string{"t"}, Usage: "filter by tag (repeatable; AND)"},
			&cli.BoolFlag{Name: "all", Usage: "include child items (attachments, notes)"},
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max items (0 = all pages)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}

			opts := zotero.ItemsOptions{
				Top:   !cmd.Bool("all"),
				Tags:  cmd.StringSlice("tag"),
				Limit: cmd.Int("limit"),
			}
			if sel := cmd.String("collection"); sel != "" {
				key, err := resolveCollectionKey(ctx, c, lib, sel)
				if err != nil {
					return err
				}
				opts.Collection = key
			}

			if opts.Limit == 0 {
				items, err := c.AllItems(ctx, lib, opts)
				if err != nil {
					return friendly(err)
				}
				return emitItems(cmd, lib, items, len(items), len(items))
			}

			items, page, err := c.Items(ctx, lib, opts)
			if err != nil {
				return friendly(err)
			}
			return emitItems(cmd, lib, items, len(items), page.TotalResults)
		},
	}
}

// emitItems prints items as a table, or in whichever machine mode was selected.
func emitItems(cmd *cli.Command, lib zotero.LibraryRef, items []zotero.Envelope, shown, total int) error {
	mode, err := outputMode(cmd)
	if err != nil {
		return err
	}
	w := out(cmd)

	if mode != output.ModeHuman {
		return emitSet(w, mode, output.KindItems, output.KindItem,
			output.NewLibrary(lib), output.NewItems(items), shown, total, items)
	}

	render.Items(w, items)
	if total > shown {
		fmt.Fprintf(w, "\n%d of %d items (use --limit 0 for all)\n", shown, total)
	} else {
		fmt.Fprintf(w, "\n%d items\n", shown)
	}
	return nil
}

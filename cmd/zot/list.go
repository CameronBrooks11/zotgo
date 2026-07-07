package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

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
				return emitItems(cmd, items, len(items), len(items))
			}

			items, page, err := c.Items(ctx, lib, opts)
			if err != nil {
				return friendly(err)
			}
			return emitItems(cmd, items, len(items), page.TotalResults)
		},
	}
}

// emitItems prints items as JSON or a table with a shown/total footer.
func emitItems(cmd *cli.Command, items []zotero.Envelope, shown, total int) error {
	w := out(cmd)
	if cmd.Bool("json") {
		return render.JSON(w, items)
	}
	render.Items(w, items)
	if total > shown {
		fmt.Fprintf(w, "\n%d of %d items (use --limit 0 for all)\n", shown, total)
	} else {
		fmt.Fprintf(w, "\n%d items\n", shown)
	}
	return nil
}

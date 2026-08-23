package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "search items by text",
		ArgsUsage: "<query>",
		Description: "Search title, creator, and year by default. Use --everything for full text and notes, " +
			"and --limit 0 for every result page.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "everything", Aliases: []string{"e"}, Usage: "search full text and notes, not just title/creator/year"},
			&cli.StringFlag{Name: "type", Usage: "filter by item type (e.g. journalArticle)"},
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max items (0 = all pages)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
			if query == "" {
				// FullName carries the invocation path, so the alias `item search`
				// hints `zot item search`, not the top-level `zot search`.
				return fmt.Errorf("missing search query (usage: %s <query>)", cmd.FullName())
			}
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}

			opts := zotero.ItemsOptions{
				Query:      query,
				Everything: cmd.Bool("everything"),
				ItemType:   cmd.String("type"),
				Limit:      cmd.Int("limit"),
			}
			if opts.Limit == 0 {
				return emitItemsAll(ctx, cmd, c, lib, opts)
			}
			items, page, err := c.Items(ctx, lib, opts)
			if err != nil {
				return friendly(err)
			}
			return emitItems(cmd, lib, items, len(items), page.TotalResults)
		},
	}
}

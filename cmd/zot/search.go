package main

import (
	"context"
	"errors"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "search items by text",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "everything", Aliases: []string{"e"}, Usage: "search full text and notes, not just title/creator/year"},
			&cli.StringFlag{Name: "type", Usage: "filter by item type (e.g. journalArticle)"},
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max items (0 = all pages)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
			if query == "" {
				return errors.New("missing search query (usage: zot search <query>)")
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

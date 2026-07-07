package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/render"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func collectionsCommand() *cli.Command {
	return &cli.Command{
		Name:    "collections",
		Aliases: []string{"cols"},
		Usage:   "list collections as a tree",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "flat", Usage: "list flat (key + name) instead of a tree"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}
			cols, err := c.AllCollections(ctx, lib, zotero.CollectionsOptions{})
			if err != nil {
				return friendly(err)
			}
			w := out(cmd)
			if cmd.Bool("json") {
				return render.JSON(w, cols)
			}
			render.Collections(w, cols, cmd.Bool("flat"))
			return nil
		},
	}
}

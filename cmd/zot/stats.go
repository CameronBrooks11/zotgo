package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/render"
)

func statsCommand() *cli.Command {
	return &cli.Command{
		Name:  "stats",
		Usage: "show library-wide counts",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}
			s, err := c.Stats(ctx, lib)
			if err != nil {
				return friendly(err)
			}
			w := out(cmd)
			if cmd.Bool("json") {
				return render.JSON(w, s)
			}
			render.Stats(w, lib.Name, s)
			return nil
		},
	}
}

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/render"
)

func statsCommand() *cli.Command {
	return &cli.Command{
		Name:  "stats",
		Usage: "show library-wide counts",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			// Stats are counts zotgo derives from Total-Results headers; there is
			// no Zotero response to pass through. Say so before doing the work.
			if mode == output.ModeRaw {
				return output.ErrRawUnavailable
			}

			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}
			s, err := c.Stats(ctx, lib)
			if err != nil {
				return friendly(err)
			}
			w := out(cmd)
			if mode != output.ModeHuman {
				return emitOne(w, mode, output.KindStats, output.NewLibrary(lib), output.NewStats(s), nil)
			}
			render.Stats(w, lib.Name, s)
			return nil
		},
	}
}

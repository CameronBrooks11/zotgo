package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "check that Zotero is running and its Local API is enabled",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			// A probe result, not a Zotero payload: --raw has nothing to show.
			if mode == output.ModeRaw {
				return output.ErrRawUnavailable
			}
			h := zotero.New(cmd.String("url")).CheckHealth(ctx)

			if mode != output.ModeHuman {
				// doctor takes no library, so the envelope carries none.
				if err := emitOne(out(cmd), mode, output.KindHealth, nil, output.NewHealth(h), nil); err != nil {
					return err
				}
			} else {
				renderHealth(out(cmd), h)
			}

			// An unreachable Zotero is a non-zero exit in every mode, so scripts
			// can branch on the status without parsing the payload.
			if h.Ready() {
				return nil
			}
			return cli.Exit("", 1)
		},
	}
}

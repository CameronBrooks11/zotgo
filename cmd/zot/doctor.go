package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "check that Zotero is running and its Local API is enabled",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			h := zotero.New(cmd.String("url")).CheckHealth(ctx)
			renderHealth(out(cmd), h)
			if h.Ready() {
				return nil
			}
			return cli.Exit("", 1)
		},
	}
}

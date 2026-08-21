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
		Usage: "check that the selected endpoint is reachable and usable",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			// A probe result, not a Zotero payload: --raw has nothing to show.
			if mode == output.ModeRaw {
				return output.ErrRawUnavailable
			}
			c, err := newClient(cmd)
			if err != nil {
				return err
			}
			h := c.CheckHealth(ctx)

			// Per-library file editability is local-only diagnosis over the
			// Connector, and never fatal: an error just omits the section.
			var libraries []zotero.LibraryFiles
			if h.Endpoint.Kind == zotero.EndpointLocal && h.ZoteroRunning {
				libraries, _ = c.LibraryFileAccess(ctx)
			}

			if mode != output.ModeHuman {
				// doctor takes no library, so the envelope carries none.
				doc := output.NewHealth(h)
				doc.Libraries = output.NewLibraryFiles(libraries)
				if err := emitOne(out(cmd), mode, output.KindHealth, nil, doc, nil); err != nil {
					return err
				}
			} else {
				renderHealth(out(cmd), h)
				renderLibraries(out(cmd), libraries)
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

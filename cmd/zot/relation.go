package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/render"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func relationCommand() *cli.Command {
	return &cli.Command{
		Name:   "relation",
		Usage:  "inspect item relations",
		Action: subcommandRequired("list"),
		Commands: []*cli.Command{
			relationListCommand(),
		},
	}
}

func relationListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "list one item's outgoing relations",
		ArgsUsage: "<item-key>",
		Description: "The complete target URI is authoritative. Stable output also provides targetKey " +
			"when the target is a strict Zotero item URI; use --raw for the complete parent envelope.",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			key := cmd.Args().First()
			if key == "" {
				return errors.New("missing item key (usage: zot relation list <item-key>)")
			}
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			client, library, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}
			raw, err := client.RawItem(ctx, library, key)
			if err != nil {
				if errors.Is(err, zotero.ErrNotFound) {
					return fmt.Errorf("no item with key %q in %s", key, library.Name)
				}
				return friendly(err)
			}
			if mode == output.ModeRaw {
				return output.WriteRaw(out(cmd), raw)
			}
			relations, err := zotero.DecodeRelations(raw)
			if err != nil {
				return fmt.Errorf("decode relations for item %q: %w", key, err)
			}
			if mode == output.ModeHuman {
				render.Relations(out(cmd), key, relations)
				return nil
			}
			records := output.NewRelations(key, relations)
			return emitSet(out(cmd), mode, output.KindRelations, output.KindRelation,
				output.NewLibrary(library), records, len(records), len(records), nil)
		},
	}
}

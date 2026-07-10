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

func showCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "show one item and its children",
		ArgsUsage: "<item-key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			key := cmd.Args().First()
			if key == "" {
				return errors.New("missing item key (usage: zot show <item-key>)")
			}
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}

			item, err := c.Item(ctx, lib, key)
			if err != nil {
				if errors.Is(err, zotero.ErrNotFound) {
					return fmt.Errorf("no item with key %q in %s", key, lib.Name)
				}
				return friendly(err)
			}
			children, _, err := c.ItemChildren(ctx, lib, key)
			if err != nil {
				return friendly(err)
			}

			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			w := out(cmd)
			if mode != output.ModeHuman {
				raw := map[string]any{"item": item, "children": children}
				return emitOne(w, mode, output.KindItem, output.NewLibrary(lib),
					output.NewItemWithChildren(item, children), raw)
			}
			render.Item(w, item, children)
			return nil
		},
	}
}

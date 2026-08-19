package main

import (
	"bytes"
	"context"
	"encoding/json"
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
		Usage:     "show one item and all of its children",
		ArgsUsage: "<item-key>",
		Description: "Stable --json and --jsonl put the shaped item at .data and its children at .data.children. " +
			"Raw output composes the complete Zotero envelopes as .item and .children.",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			key := cmd.Args().First()
			if key == "" {
				return errors.New("missing item key (usage: zot show <item-key>)")
			}
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}

			rawItem, rawChildren, err := c.RawItemWithChildren(ctx, lib, key)
			if err != nil {
				if errors.Is(err, zotero.ErrNotFound) {
					return fmt.Errorf("no item with key %q in %s", key, lib.Name)
				}
				return friendly(err)
			}
			if mode == output.ModeRaw {
				_, err = out(cmd).Write(rawShowDocument(rawItem, rawChildren))
				return err
			}

			var item zotero.Envelope
			if err := json.Unmarshal(rawItem, &item); err != nil {
				return fmt.Errorf("decode item %q: %w", key, err)
			}
			children := make([]zotero.Envelope, len(rawChildren))
			for i := range rawChildren {
				if err := json.Unmarshal(rawChildren[i], &children[i]); err != nil {
					return fmt.Errorf("decode child item %d: %w", i, err)
				}
			}
			w := out(cmd)
			if mode != output.ModeHuman {
				return emitOne(w, mode, output.KindItem, output.NewLibrary(lib),
					output.NewItemWithChildren(item, children), nil)
			}
			render.Item(w, item, children)
			return nil
		},
	}
}

func rawShowDocument(item json.RawMessage, children []json.RawMessage) []byte {
	var document bytes.Buffer
	document.WriteString(`{"item":`)
	document.Write(bytes.TrimSpace(item))
	document.WriteString(`,"children":[`)
	for i, child := range children {
		if i > 0 {
			document.WriteByte(',')
		}
		document.Write(bytes.TrimSpace(child))
	}
	document.WriteString("]}\n")
	return document.Bytes()
}

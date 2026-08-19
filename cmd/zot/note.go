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

func noteCommand() *cli.Command {
	return &cli.Command{
		Name:  "note",
		Usage: "inspect Zotero notes",
		Commands: []*cli.Command{
			noteShowCommand(),
		},
	}
}

func noteShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "show one note including its rich-text HTML",
		ArgsUsage: "<note-key>",
		Description: "Stable output preserves Zotero's data.note HTML exactly in the html field. " +
			"Use --raw for the complete Zotero item envelope.",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			key := cmd.Args().First()
			if key == "" {
				return errors.New("missing note key (usage: zot note show <note-key>)")
			}
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}
			c, lib, err := resolveLibrary(ctx, cmd)
			if err != nil {
				return err
			}
			w := out(cmd)
			if mode == output.ModeRaw {
				raw, err := c.RawItem(ctx, lib, key)
				if err != nil {
					return noteReadError(err, key, lib.Name)
				}
				if err := zotero.RequireItemType(raw, "note"); err != nil {
					return fmt.Errorf("decode note %q: %w", key, err)
				}
				return output.WriteRaw(w, raw)
			}
			note, err := c.Note(ctx, lib, key)
			if err != nil {
				return noteReadError(err, key, lib.Name)
			}
			if mode == output.ModeHuman {
				render.Note(w, note)
				return nil
			}
			return emitOne(w, mode, output.KindNote, output.NewLibrary(lib), output.NewNote(note), nil)
		},
	}
}

func noteReadError(err error, key, library string) error {
	if errors.Is(err, zotero.ErrNotFound) {
		return fmt.Errorf("no note with key %q in %s", key, library)
	}
	return friendly(err)
}

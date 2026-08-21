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

func attachmentCommand() *cli.Command {
	return &cli.Command{
		Name:  "attachment",
		Usage: "inspect and import attachments",
		Description: "Use `attachment show` for API-reported metadata, or `attachment import` to attach a local file " +
			"as a Zotero-managed file. Managed imports are local-only.",
		Action: subcommandRequired("show"),
		Commands: []*cli.Command{
			attachmentShowCommand(),
			attachmentImportCommand(),
		},
	}
}

func attachmentShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "show attachment metadata",
		ArgsUsage: "<attachment-key>",
		Description: "Reports metadata Zotero advertised without opening its database or downloading the file. " +
			"Use --raw for the complete Zotero envelope.",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			key := cmd.Args().First()
			if key == "" {
				return errors.New("missing attachment key (usage: zot attachment show <attachment-key>)")
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
					if errors.Is(err, zotero.ErrNotFound) {
						return fmt.Errorf("no attachment with key %q in %s", key, lib.Name)
					}
					return friendly(err)
				}
				if err := zotero.RequireItemType(raw, "attachment"); err != nil {
					return fmt.Errorf("decode attachment %q: %w", key, err)
				}
				return output.WriteRaw(w, raw)
			}
			attachment, err := c.Attachment(ctx, lib, key)
			if err != nil {
				if errors.Is(err, zotero.ErrNotFound) {
					return fmt.Errorf("no attachment with key %q in %s", key, lib.Name)
				}
				return friendly(err)
			}
			if mode == output.ModeHuman {
				render.Attachment(w, attachment)
				return nil
			}
			return emitOne(w, mode, output.KindAttachment, output.NewLibrary(lib), output.NewAttachment(attachment), nil)
		},
	}
}

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

func annotationCommand() *cli.Command {
	return &cli.Command{
		Name:   "annotation",
		Usage:  "inspect attachment annotations",
		Action: subcommandRequired("list"),
		Commands: []*cli.Command{
			annotationListCommand(),
		},
	}
}

func annotationListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "list annotations belonging to an attachment",
		ArgsUsage: "<attachment-key>",
		Description: "Stable output contains compact metadata in document order, without annotation text, " +
			"comments, position data, or image bytes. Use --raw for Zotero's complete annotation envelopes.",
		Action: annotationListAction,
	}
}

func annotationListAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 || args[0] == "" {
		return errors.New("missing attachment key (usage: zot annotation list <attachment-key>)")
	}
	if len(args) != 1 {
		return errors.New("annotation list accepts exactly one attachment key")
	}
	mode, err := outputMode(cmd)
	if err != nil {
		return err
	}
	attachmentKey := args[0]
	client, library, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	rawAnnotations, err := client.AllRawAnnotations(ctx, library, attachmentKey)
	if err != nil {
		return annotationReadError(err, attachmentKey, library.Name)
	}
	writer := out(cmd)
	if mode == output.ModeRaw {
		return output.WriteRaw(writer, rawAnnotations)
	}
	annotations, err := zotero.DecodeAnnotations(rawAnnotations, attachmentKey)
	if err != nil {
		return fmt.Errorf("decode annotations for attachment %q: %w", attachmentKey, err)
	}
	if mode == output.ModeHuman {
		render.Annotations(writer, attachmentKey, annotations)
		return nil
	}
	records := output.NewAnnotations(annotations)
	return emitSet(writer, mode, output.KindAnnotations, output.KindAnnotation,
		output.NewLibrary(library), records, len(records), len(records), nil)
}

func annotationReadError(err error, key, library string) error {
	if errors.Is(err, zotero.ErrNotFound) {
		return fmt.Errorf("no attachment with key %q in %s", key, library)
	}
	return friendly(err)
}

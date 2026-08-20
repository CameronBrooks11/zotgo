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

func collectionPathCommand() *cli.Command {
	return &cli.Command{
		Name:      "path",
		Usage:     "resolve collection ancestry from root to leaf",
		ArgsUsage: "<collection-key>...",
		Description: "Resolves one or more collection keys in request order. Stable output uses an unambiguous " +
			"path array; --raw is unavailable because paths are derived from multiple collection records.",
		Action: collectionPathAction,
	}
}

func collectionPathAction(ctx context.Context, cmd *cli.Command) error {
	keys := cmd.Args().Slice()
	if len(keys) == 0 {
		return errors.New("missing collection key (usage: zot collection path <collection-key>...)")
	}
	mode, err := outputMode(cmd)
	if err != nil {
		return err
	}
	if mode == output.ModeRaw {
		return fmt.Errorf("%w: collection paths are derived from multiple collection records", output.ErrRawUnavailable)
	}
	client, library, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	collections, err := client.AllCollections(ctx, library, zotero.CollectionsOptions{})
	if err != nil {
		return friendly(err)
	}
	paths, err := zotero.ResolveCollectionPaths(collections, keys)
	if err != nil {
		return fmt.Errorf("resolve collection paths: %w", err)
	}
	if mode == output.ModeHuman {
		render.CollectionPaths(out(cmd), paths)
		return nil
	}
	records := output.NewCollectionPaths(paths)
	return emitSet(out(cmd), mode, output.KindCollections, output.KindCollection,
		output.NewLibrary(library), records, len(records), len(records), nil)
}

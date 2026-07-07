package main

import (
	"context"
	"fmt"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// resolveCollectionKey turns a --collection selector (an exact key or an exact
// name) into a collection key, failing clearly on no match or an ambiguous name.
func resolveCollectionKey(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, selector string) (string, error) {
	cols, err := c.AllCollections(ctx, lib, zotero.CollectionsOptions{})
	if err != nil {
		return "", friendly(err)
	}
	var byName []string
	for _, col := range cols {
		if col.Key == selector {
			return selector, nil
		}
		data, err := col.CollectionData()
		if err != nil {
			continue
		}
		if data.Name == selector {
			byName = append(byName, col.Key)
		}
	}
	switch len(byName) {
	case 0:
		return "", fmt.Errorf("no collection matching %q", selector)
	case 1:
		return byName[0], nil
	default:
		return "", fmt.Errorf("collection name %q is ambiguous (%d matches); use its key", selector, len(byName))
	}
}

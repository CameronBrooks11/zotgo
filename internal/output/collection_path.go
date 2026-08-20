package output

import "github.com/CameronBrooks11/zotgo/internal/zotero"

// NewCollectionPaths converts resolved ancestry to collection-compatible DTOs.
func NewCollectionPaths(paths []zotero.CollectionPath) []Collection {
	records := make([]Collection, 0, len(paths))
	for _, path := range paths {
		segments := make([]CollectionPathSegment, 0, len(path.Segments))
		for _, segment := range path.Segments {
			segments = append(segments, CollectionPathSegment{Key: segment.Key, Name: segment.Name})
		}
		records = append(records, Collection{
			Key:       path.Key,
			Name:      path.Name,
			ParentKey: path.ParentKey,
			NumItems:  path.NumItems,
			Path:      segments,
		})
	}
	return records
}

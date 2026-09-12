package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Fields that must never travel with a copy, grouped by why.
//
// Server-assigned: Zotero owns these and rejects or ignores a caller's values.
// Source-bound: they name things in the source library and would be wrong, or
// dangling, in the target. Upload-managed: the attachment upload protocol sets
// them once the bytes land, and setting them early is refused outright.
var (
	serverAssignedFields = []string{"key", "version", "dateAdded", "dateModified"}
	sourceBoundFields    = []string{"relations", "collections", "parentItem"}
	uploadManagedFields  = []string{"filename", "md5", "mtime"}
)

// copyPayload builds the create payload for one object in a copy.
//
// It works from Zotero's own `data` object rather than a zotgo DTO, because that
// is the vocabulary a write is validated against — the DTO is a read contract and
// does not round-trip (see docs/machine-output.md).
//
// parentKey reparents a child onto the newly created parent; it is empty for a
// top-level item. collections replaces the source's membership, which points at
// collections that do not exist in the target library.
//
// Annotation fields are deliberately untouched. annotationPosition and
// annotationSortIndex are opaque page geometry, meaningful only against the
// document they describe — which travels with them here — so carrying them
// verbatim is both correct and the only safe option.
func copyPayload(data json.RawMessage, parentKey string, collections []string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("decode source item data: %w", err)
	}
	if _, ok := fields["itemType"]; !ok {
		// Without itemType Zotero rejects the create, and the resulting error
		// names a field the user never supplied. Fail here, where the reason is
		// still legible.
		return nil, fmt.Errorf("source item data has no itemType")
	}

	drop := append(append([]string{}, serverAssignedFields...), sourceBoundFields...)
	if itemTypeOf(fields) == "attachment" {
		drop = append(drop, uploadManagedFields...)
	}
	for _, name := range drop {
		delete(fields, name)
	}

	if parentKey != "" {
		encoded, err := json.Marshal(parentKey)
		if err != nil {
			return nil, err
		}
		fields["parentItem"] = encoded
	}
	if len(collections) > 0 {
		encoded, err := json.Marshal(collections)
		if err != nil {
			return nil, err
		}
		fields["collections"] = encoded
	}

	return marshalStable(fields)
}

// itemTypeOf reads itemType without failing; callers have already established it
// is present.
func itemTypeOf(fields map[string]json.RawMessage) string {
	var itemType string
	if raw, ok := fields["itemType"]; ok {
		_ = json.Unmarshal(raw, &itemType)
	}
	return itemType
}

// marshalStable encodes with sorted keys so the same source item always produces
// the same bytes. Dry-run output and tests both compare payloads, and Go's map
// iteration order would otherwise make them flap.
func marshalStable(fields map[string]json.RawMessage) (json.RawMessage, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	out := []byte{'{'}
	for i, name := range names {
		if i > 0 {
			out = append(out, ',')
		}
		encodedName, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		out = append(out, encodedName...)
		out = append(out, ':')
		out = append(out, fields[name]...)
	}
	return append(out, '}'), nil
}

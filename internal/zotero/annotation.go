package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// AllRawAnnotations reads every direct annotation child of one attachment. It
// validates that the parent is an attachment and honors the parent response's
// backoff before requesting children, then returns complete annotation envelopes
// in server order — or no partial result if any page fails. It encapsulates the
// parent-fetch + validate + backoff + child-fetch dance behind one call, matching
// how Note/Attachment/RawItemWithChildren expose their reads.
func (c *Client) AllRawAnnotations(ctx context.Context, library LibraryRef, attachmentKey string) ([]json.RawMessage, error) {
	parent, page, err := c.rawItemPage(ctx, library, attachmentKey)
	if err != nil {
		return nil, err
	}
	if err := RequireItemType(parent, "attachment"); err != nil {
		return nil, fmt.Errorf("annotation parent %q: %w", attachmentKey, err)
	}
	if err := c.honorBackoff(ctx, page); err != nil {
		return nil, err
	}
	return c.AllRawChildItems(ctx, library, attachmentKey, ChildItemsOptions{ItemType: "annotation", Limit: 100})
}

// Annotation is compact metadata for one attachment annotation. Text, comments,
// position data, and image bytes remain outside this listing model.
type Annotation struct {
	Key           string
	AttachmentKey string
	Type          string
	PageLabel     string
	Color         string
	SortIndex     string
	HasText       bool
	HasComment    bool
}

// DecodeAnnotation validates and decodes compact annotation metadata from one
// complete Zotero item envelope.
func DecodeAnnotation(raw json.RawMessage) (Annotation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Annotation{}, errors.New("expected an annotation item object")
	}
	var item struct {
		Key  string          `json:"key"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &item); err != nil {
		return Annotation{}, err
	}
	if item.Key == "" {
		return Annotation{}, errors.New("missing annotation key")
	}
	dataBytes := bytes.TrimSpace(item.Data)
	if len(dataBytes) == 0 || dataBytes[0] != '{' {
		return Annotation{}, fmt.Errorf("annotation %s has no data object", item.Key)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return Annotation{}, fmt.Errorf("annotation %s: %w", item.Key, err)
	}
	dataKey, err := annotationString(data, "key", true)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	if dataKey != item.Key {
		return Annotation{}, fmt.Errorf("annotation %s data has key %q", item.Key, dataKey)
	}
	itemType, err := annotationString(data, "itemType", true)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	if itemType != "annotation" {
		return Annotation{}, fmt.Errorf("item %s has type %q, not annotation", item.Key, itemType)
	}
	parent, err := annotationString(data, "parentItem", true)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	annotationType, err := annotationString(data, "annotationType", true)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	pageLabel, err := annotationString(data, "annotationPageLabel", false)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	color, err := annotationString(data, "annotationColor", false)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	sortIndex, err := annotationString(data, "annotationSortIndex", false)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	text, err := annotationString(data, "annotationText", false)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	comment, err := annotationString(data, "annotationComment", false)
	if err != nil {
		return Annotation{}, fmt.Errorf("annotation %s %w", item.Key, err)
	}
	return Annotation{
		Key: item.Key, AttachmentKey: parent, Type: annotationType,
		PageLabel: pageLabel, Color: color, SortIndex: sortIndex,
		HasText: text != "", HasComment: comment != "",
	}, nil
}

func annotationString(data map[string]json.RawMessage, field string, required bool) (string, error) {
	raw, ok := data[field]
	if !ok {
		if required {
			return "", fmt.Errorf("has no %s", field)
		}
		return "", nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("%s must be a string, not null", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string: %w", field, err)
	}
	if required && value == "" {
		return "", fmt.Errorf("has no %s", field)
	}
	return value, nil
}

// DecodeAnnotations validates one attachment's annotations and returns them in
// document order: sort index first, key as a deterministic tie-breaker, and
// missing sort indexes last.
func DecodeAnnotations(rawItems []json.RawMessage, attachmentKey string) ([]Annotation, error) {
	annotations := make([]Annotation, 0, len(rawItems))
	for i, raw := range rawItems {
		annotation, err := DecodeAnnotation(raw)
		if err != nil {
			return nil, fmt.Errorf("decode annotation %d: %w", i, err)
		}
		if annotation.AttachmentKey != attachmentKey {
			return nil, fmt.Errorf("annotation %q reports parent %q, expected %q", annotation.Key, annotation.AttachmentKey, attachmentKey)
		}
		annotations = append(annotations, annotation)
	}
	sort.Slice(annotations, func(i, j int) bool {
		left, right := annotations[i], annotations[j]
		if left.SortIndex == "" || right.SortIndex == "" {
			if left.SortIndex == "" && right.SortIndex != "" {
				return false
			}
			if left.SortIndex != "" && right.SortIndex == "" {
				return true
			}
		}
		if left.SortIndex == right.SortIndex {
			return left.Key < right.Key
		}
		return left.SortIndex < right.SortIndex
	})
	return annotations, nil
}

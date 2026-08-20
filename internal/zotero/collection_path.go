package zotero

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// CollectionPathSegment is one collection in a root-to-leaf path.
type CollectionPathSegment struct {
	Key  string
	Name string
}

// CollectionPath is one requested collection and its complete ancestry.
type CollectionPath struct {
	Key       string
	Name      string
	ParentKey string
	NumItems  int
	Segments  []CollectionPathSegment
}

// ResolveCollectionPaths resolves requested keys from a complete collection
// listing. Results preserve request order and duplicates.
func ResolveCollectionPaths(envelopes []Envelope, keys []string) ([]CollectionPath, error) {
	collections := make(map[string]Envelope, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.Key == "" {
			return nil, fmt.Errorf("collection response has no key")
		}
		if _, exists := collections[envelope.Key]; exists {
			return nil, fmt.Errorf("collection response contains duplicate key %q", envelope.Key)
		}
		collections[envelope.Key] = envelope
	}

	paths := make([]CollectionPath, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("collection key must not be empty")
		}
		path, err := resolveCollectionPath(collections, key)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func resolveCollectionPath(collections map[string]Envelope, requestedKey string) (CollectionPath, error) {
	visited := make(map[string]struct{})
	reversed := make([]CollectionPathSegment, 0)
	currentKey := requestedKey
	var path CollectionPath
	for currentKey != "" {
		if _, seen := visited[currentKey]; seen {
			return CollectionPath{}, fmt.Errorf("collection path for %q contains a cycle at %q", requestedKey, currentKey)
		}
		visited[currentKey] = struct{}{}

		envelope, ok := collections[currentKey]
		if !ok {
			if currentKey == requestedKey {
				return CollectionPath{}, fmt.Errorf("no collection with key %q", requestedKey)
			}
			return CollectionPath{}, fmt.Errorf("collection path for %q references missing parent %q", requestedKey, currentKey)
		}
		data, parentKey, err := strictCollectionData(envelope)
		if err != nil {
			return CollectionPath{}, err
		}
		if currentKey == requestedKey {
			path = CollectionPath{
				Key:       requestedKey,
				Name:      data.Name,
				ParentKey: parentKey,
				NumItems:  collectionNumItems(envelope),
			}
		}
		reversed = append(reversed, CollectionPathSegment{Key: currentKey, Name: data.Name})
		currentKey = parentKey
	}

	path.Segments = make([]CollectionPathSegment, len(reversed))
	for i := range reversed {
		path.Segments[len(reversed)-1-i] = reversed[i]
	}
	return path, nil
}

func strictCollectionData(envelope Envelope) (CollectionData, string, error) {
	trimmed := bytes.TrimSpace(envelope.Data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return CollectionData{}, "", fmt.Errorf("decode collection %q: expected a data object", envelope.Key)
	}
	var data CollectionData
	if err := json.Unmarshal(trimmed, &data); err != nil {
		return CollectionData{}, "", fmt.Errorf("decode collection %q: %w", envelope.Key, err)
	}
	if data.Key == "" {
		return CollectionData{}, "", fmt.Errorf("collection %q data has no key", envelope.Key)
	}
	if data.Key != envelope.Key {
		return CollectionData{}, "", fmt.Errorf("collection %q data has key %q", envelope.Key, data.Key)
	}
	if data.Name == "" {
		return CollectionData{}, "", fmt.Errorf("collection %q data has no name", envelope.Key)
	}
	parentKey, err := strictCollectionParentKey(data.ParentCollection)
	if err != nil {
		return CollectionData{}, "", fmt.Errorf("collection %q parentCollection: %w", envelope.Key, err)
	}
	return data, parentKey, nil
}

func strictCollectionParentKey(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("false")) {
		return "", nil
	}
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", fmt.Errorf("expected a non-empty collection key or false")
	}
	var key string
	if err := json.Unmarshal(trimmed, &key); err != nil || key == "" {
		return "", fmt.Errorf("expected a non-empty collection key or false")
	}
	return key, nil
}

func collectionNumItems(envelope Envelope) int {
	raw := envelope.Meta["numItems"]
	var count int
	if err := json.Unmarshal(raw, &count); err == nil {
		return count
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		count, _ = strconv.Atoi(text)
	}
	return count
}

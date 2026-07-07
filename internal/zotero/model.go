package zotero

import (
	"encoding/json"
	"strconv"
)

// Envelope is the common Local API wrapper for items and collections.
//
// Data is intentionally raw: Zotero item fields vary by itemType, and preserving
// unknown fields is safer than flattening them away.
type Envelope struct {
	Key     string                     `json:"key"`
	Version int                        `json:"version"`
	Library Library                    `json:"library"`
	Links   map[string]Link            `json:"links"`
	Meta    map[string]json.RawMessage `json:"meta"`
	Data    json.RawMessage            `json:"data"`
}

// Library identifies the library that owns an envelope.
type Library struct {
	Type  string          `json:"type"`
	ID    int64           `json:"id"`
	Name  string          `json:"name"`
	Links map[string]Link `json:"links"`
}

// Link is a Zotero Local API link object. Attachment links include extra fields.
type Link struct {
	Href           string `json:"href"`
	Type           string `json:"type"`
	AttachmentType string `json:"attachmentType"`
	AttachmentSize int64  `json:"attachmentSize"`
}

// ItemData contains the stable item fields zotgo needs for display and tests.
type ItemData struct {
	Key         string    `json:"key"`
	Version     int       `json:"version"`
	ItemType    string    `json:"itemType"`
	Title       string    `json:"title"`
	Creators    []Creator `json:"creators"`
	Tags        []Tag     `json:"tags"`
	Collections []string  `json:"collections"`
}

type Creator struct {
	CreatorType string `json:"creatorType"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Name        string `json:"name"`
}

type Tag struct {
	Tag  string `json:"tag"`
	Type int    `json:"type"`
}

// CollectionData contains the stable collection fields used for tree rendering.
type CollectionData struct {
	Key              string          `json:"key"`
	Name             string          `json:"name"`
	ParentCollection json.RawMessage `json:"parentCollection"`
}

// ItemData decodes e.Data as Zotero item JSON.
func (e Envelope) ItemData() (ItemData, error) {
	var data ItemData
	if len(e.Data) == 0 {
		return data, nil
	}
	err := json.Unmarshal(e.Data, &data)
	return data, err
}

// CollectionData decodes e.Data as Zotero collection JSON.
func (e Envelope) CollectionData() (CollectionData, error) {
	var data CollectionData
	if len(e.Data) == 0 {
		return data, nil
	}
	err := json.Unmarshal(e.Data, &data)
	return data, err
}

// Title is a convenience accessor for item titles and collection names.
func (e Envelope) Title() string {
	if item, err := e.ItemData(); err == nil && item.Title != "" {
		return item.Title
	}
	if collection, err := e.CollectionData(); err == nil {
		return collection.Name
	}
	return ""
}

// ItemType returns data.itemType when this envelope wraps an item.
func (e Envelope) ItemType() string {
	item, err := e.ItemData()
	if err != nil {
		return ""
	}
	return item.ItemType
}

// CreatorSummary returns meta.creatorSummary.
func (e Envelope) CreatorSummary() string {
	return rawString(e.Meta["creatorSummary"])
}

// ParsedDate returns meta.parsedDate.
func (e Envelope) ParsedDate() string {
	return rawString(e.Meta["parsedDate"])
}

// NumChildren returns meta.numChildren. Missing/non-numeric values return 0.
func (e Envelope) NumChildren() int {
	raw, ok := e.Meta["numChildren"]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, _ = strconv.Atoi(s)
	}
	return n
}

// ParentKey returns data.parentCollection as a collection key, or "" for top-level collections.
func (d CollectionData) ParentKey() string {
	var s string
	if err := json.Unmarshal(d.ParentCollection, &s); err == nil {
		return s
	}
	return ""
}

func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 {
		return ""
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func itemEnvelope() zotero.Envelope {
	return zotero.Envelope{
		Key:     "AAAA1111",
		Version: 7,
		Meta: map[string]json.RawMessage{
			"creatorSummary": json.RawMessage(`"Posten"`),
			"parsedDate":     json.RawMessage(`"2009-05"`),
			"numChildren":    json.RawMessage(`2`),
		},
		Data: json.RawMessage(`{
			"key": "AAAA1111",
			"itemType": "journalArticle",
			"title": "Algae paper",
			"date": "May 2009",
			"creators": [
				{"creatorType":"author","firstName":"Clemens","lastName":"Posten"},
				{"creatorType":"author","name":"Institute of Things"}
			],
			"tags": [{"tag":"algae","type":0},{"tag":"imported","type":1}],
			"collections": ["COLL0001"]
		}`),
	}
}

func TestNewItem_FlattensEnvelope(t *testing.T) {
	got := NewItem(itemEnvelope())

	if got.Key != "AAAA1111" || got.Version != 7 {
		t.Fatalf("key/version = %q/%d", got.Key, got.Version)
	}
	if got.Type != "journalArticle" || got.Title != "Algae paper" {
		t.Fatalf("type/title = %q/%q", got.Type, got.Title)
	}
	if got.Date != "May 2009" || got.ParsedDate != "2009-05" {
		t.Fatalf("date/parsedDate = %q/%q", got.Date, got.ParsedDate)
	}
	if got.CreatorSummary != "Posten" || got.NumChildren != 2 {
		t.Fatalf("creatorSummary/numChildren = %q/%d", got.CreatorSummary, got.NumChildren)
	}
	if len(got.Collections) != 1 || got.Collections[0] != "COLL0001" {
		t.Fatalf("collections = %v", got.Collections)
	}
}

// Zotero stores a creator as first/last OR as a single name. Both survive.
func TestNewItem_CreatorForms(t *testing.T) {
	got := NewItem(itemEnvelope())
	if len(got.Creators) != 2 {
		t.Fatalf("len(creators) = %d, want 2", len(got.Creators))
	}
	if c := got.Creators[0]; c.FirstName != "Clemens" || c.LastName != "Posten" || c.Name != "" {
		t.Fatalf("two-field creator = %+v", c)
	}
	if c := got.Creators[1]; c.Name != "Institute of Things" || c.LastName != "" {
		t.Fatalf("single-field creator = %+v", c)
	}
}

// Tag type 1 is Zotero's own; 0 is a human's.
func TestNewItem_TagAutomaticFlag(t *testing.T) {
	got := NewItem(itemEnvelope())
	if len(got.Tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2", len(got.Tags))
	}
	if got.Tags[0] != (Tag{Name: "algae", Automatic: false}) {
		t.Fatalf("manual tag = %+v", got.Tags[0])
	}
	if got.Tags[1] != (Tag{Name: "imported", Automatic: true}) {
		t.Fatalf("automatic tag = %+v", got.Tags[1])
	}
}

// Slices must marshal as [] rather than null: a script iterating the field
// should not have to special-case the empty case.
func TestNewItem_EmptySlicesAreNotNull(t *testing.T) {
	got := NewItem(zotero.Envelope{Key: "K", Data: json.RawMessage(`{"itemType":"book"}`)})

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, field := range []string{`"creators":[]`, `"tags":[]`, `"collections":[]`} {
		if !strings.Contains(string(blob), field) {
			t.Errorf("missing %s in %s", field, blob)
		}
	}
	if strings.Contains(string(blob), "null") {
		t.Errorf("null in output: %s", blob)
	}
}

// A malformed data payload must not lose the envelope's trustworthy fields.
func TestNewItem_MalformedDataKeepsEnvelopeFields(t *testing.T) {
	got := NewItem(zotero.Envelope{Key: "K", Version: 3, Data: json.RawMessage(`"not an object"`)})
	if got.Key != "K" || got.Version != 3 {
		t.Fatalf("envelope fields lost: %+v", got)
	}
}

func TestNewCollection(t *testing.T) {
	e := zotero.Envelope{
		Key:     "COLL0002",
		Version: 4,
		Meta:    map[string]json.RawMessage{"numItems": json.RawMessage(`9`)},
		Data:    json.RawMessage(`{"key":"COLL0002","name":"Polyhedra","parentCollection":"COLL0001"}`),
	}
	got := NewCollection(e)
	want := Collection{Key: "COLL0002", Version: 4, Name: "Polyhedra", ParentKey: "COLL0001", NumItems: 9}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// A top-level collection has parentCollection: false, not a key.
func TestNewCollection_TopLevelHasNoParent(t *testing.T) {
	e := zotero.Envelope{Key: "C", Data: json.RawMessage(`{"name":"Top","parentCollection":false}`)}
	if got := NewCollection(e); got.ParentKey != "" {
		t.Fatalf("ParentKey = %q, want empty", got.ParentKey)
	}
}

func TestWriteJSON_EnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	lib := &Library{Type: "user", ID: 0, Name: "My Library"}
	doc := NewDocument(KindItems, lib, []Item{{Key: "A"}}).WithMeta(1, 42)

	if err := WriteJSON(&buf, doc); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["schema"] != float64(SchemaVersion) {
		t.Errorf("schema = %v, want %d", got["schema"], SchemaVersion)
	}
	if got["kind"] != "items" {
		t.Errorf("kind = %v", got["kind"])
	}
	meta, ok := got["meta"].(map[string]any)
	if !ok || meta["shown"] != float64(1) || meta["total"] != float64(42) {
		t.Errorf("meta = %v", got["meta"])
	}
	if _, ok := got["data"].([]any); !ok {
		t.Errorf("data is not an array: %T", got["data"])
	}
}

// Meta is absent, not zeroed, when the notion does not apply.
func TestWriteJSON_OmitsMetaWhenUnset(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, NewDocument(KindStats, nil, Stats{})); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if strings.Contains(buf.String(), "meta") || strings.Contains(buf.String(), "library") {
		t.Fatalf("unset fields present:\n%s", buf.String())
	}
}

// The point of the jsonl design: every line stands alone.
func TestWriteJSONL_EachLineIsSelfDescribing(t *testing.T) {
	var buf bytes.Buffer
	lib := &Library{Type: "user", Name: "My Library"}
	items := []Item{{Key: "A"}, {Key: "B"}, {Key: "C"}}

	if err := WriteJSONL(&buf, KindItem, lib, items); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if doc["schema"] != float64(SchemaVersion) {
			t.Errorf("line %d lacks schema: %s", i, line)
		}
		if doc["kind"] != "item" {
			t.Errorf("line %d kind = %v, want the singular 'item'", i, doc["kind"])
		}
		if doc["library"] == nil {
			t.Errorf("line %d lacks library: %s", i, line)
		}
	}
}

// A record must never be split across lines, or the format breaks.
func TestWriteJSONL_RecordsAreNotIndented(t *testing.T) {
	var buf bytes.Buffer
	items := []Item{{Key: "A", Title: "A title"}}
	if err := WriteJSONL(&buf, KindItem, nil, items); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
	if n := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n"); n != 0 {
		t.Fatalf("record spans %d newlines:\n%s", n+1, buf.String())
	}
}

func TestWriteJSONL_EmptyWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSONL(&buf, KindItem, nil, []Item{}); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %q, want nothing", buf.String())
	}
}

func TestResolveMode(t *testing.T) {
	tests := []struct {
		name                string
		jsonF, jsonlF, rawF bool
		want                Mode
		wantErr             bool
	}{
		{name: "none", want: ModeHuman},
		{name: "json", jsonF: true, want: ModeJSON},
		{name: "jsonl", jsonlF: true, want: ModeJSONL},
		{name: "raw", rawF: true, want: ModeRaw},
		{name: "json+jsonl", jsonF: true, jsonlF: true, wantErr: true},
		{name: "json+raw", jsonF: true, rawF: true, wantErr: true},
		{name: "jsonl+raw", jsonlF: true, rawF: true, wantErr: true},
		{name: "all three", jsonF: true, jsonlF: true, rawF: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveMode(tt.jsonF, tt.jsonlF, tt.rawF)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected a mutual-exclusion error")
				}
				if !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("unhelpful error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestNewNotePreservesExactHTMLAndArrays(t *testing.T) {
	record := NewNote(zotero.Note{
		Key:          "NOTE0001",
		ParentKey:    "PARENT01",
		DateAdded:    "2026-01-01T00:00:00Z",
		DateModified: "2026-01-02T00:00:00Z",
		Tags:         []zotero.Tag{{Tag: "review", Type: 1}},
		HTML:         `<div data-schema-version="9"><p>Body</p></div>`,
	})
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{`"key":"NOTE0001"`, `"parentKey":"PARENT01"`, `"automatic":true`} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q: %s", want, text)
		}
	}
	var decoded Note
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.HTML != record.HTML {
		t.Fatalf("HTML changed: got %q, want %q", decoded.HTML, record.HTML)
	}

	empty, err := json.Marshal(NewNote(zotero.Note{Key: "EMPTY001", Tags: []zotero.Tag{}, HTML: ""}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"tags":[]`) || !strings.Contains(string(empty), `"html":""`) || !strings.Contains(string(empty), `"parentKey":""`) {
		t.Fatalf("empty note fields = %s", empty)
	}
}

package zotero

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeNote(t *testing.T) {
	raw := json.RawMessage(`{
		"key":"NOTE0001",
		"futureTop":true,
		"data":{
			"itemType":"note",
			"parentItem":"PARENT01",
			"dateAdded":"2026-01-01T00:00:00Z",
			"dateModified":"2026-01-02T00:00:00Z",
			"tags":[{"tag":"review","type":0}],
			"note":"<div data-schema-version=\"9\"><p>Body</p></div>",
			"futureData":7
		}
	}`)
	note, err := DecodeNote(raw)
	if err != nil {
		t.Fatalf("DecodeNote: %v", err)
	}
	if note.Key != "NOTE0001" || note.ParentKey != "PARENT01" || note.DateAdded != "2026-01-01T00:00:00Z" || note.DateModified != "2026-01-02T00:00:00Z" {
		t.Fatalf("note identity/dates = %#v", note)
	}
	if note.HTML != `<div data-schema-version="9"><p>Body</p></div>` {
		t.Fatalf("HTML = %q", note.HTML)
	}
	if len(note.Tags) != 1 || note.Tags[0].Tag != "review" {
		t.Fatalf("tags = %#v", note.Tags)
	}
}

func TestDecodeNoteAcceptsStandaloneEmptyNote(t *testing.T) {
	for _, parent := range []string{"", `,"parentItem":false`, `,"parentItem":null`} {
		raw := json.RawMessage(`{"key":"EMPTY001","data":{"itemType":"note","note":""` + parent + `}}`)
		note, err := DecodeNote(raw)
		if err != nil {
			t.Fatalf("parent %q: %v", parent, err)
		}
		if note.ParentKey != "" || note.HTML != "" || note.Tags == nil {
			t.Fatalf("parent %q: note = %#v", parent, note)
		}
	}
}

func TestDecodeNoteRejectsMalformedStableFields(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing key", raw: `{"data":{"itemType":"note","note":""}}`, want: "missing note key"},
		{name: "missing data", raw: `{"key":"NOTE0001"}`, want: "has no data"},
		{name: "wrong type", raw: `{"key":"NOTE0001","data":{"itemType":"book","note":""}}`, want: `type "book"`},
		{name: "bad parent", raw: `{"key":"NOTE0001","data":{"itemType":"note","parentItem":{},"note":""}}`, want: "parentItem"},
		{name: "missing note", raw: `{"key":"NOTE0001","data":{"itemType":"note"}}`, want: "has no note field"},
		{name: "null note", raw: `{"key":"NOTE0001","data":{"itemType":"note","note":null}}`, want: "must be a string"},
		{name: "numeric note", raw: `{"key":"NOTE0001","data":{"itemType":"note","note":42}}`, want: "must be a string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeNote(json.RawMessage(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestNewCollectionPathsUsesCollectionContract(t *testing.T) {
	records := NewCollectionPaths([]zotero.CollectionPath{{
		Key: "LEAF0001", Name: "Methods", ParentKey: "ROOT0001", NumItems: 9,
		Segments: []zotero.CollectionPathSegment{
			{Key: "ROOT0001", Name: "Research"},
			{Key: "LEAF0001", Name: "Methods"},
		},
	}})
	if len(records) != 1 || records[0].Key != "LEAF0001" || records[0].NumItems != 9 || len(records[0].Path) != 2 {
		t.Fatalf("records = %#v", records)
	}
	encoded, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"key":"LEAF0001"`, `"parentKey":"ROOT0001"`, `"numItems":9`, `"path":[`, `"name":"Research"`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("path output missing %q: %s", want, encoded)
		}
	}
}

func TestCollectionPathFieldIsAbsentWhenNotFetched(t *testing.T) {
	record := NewCollection(zotero.Envelope{
		Key:  "ROOT0001",
		Data: json.RawMessage(`{"key":"ROOT0001","name":"Root","parentCollection":false}`),
	})
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"path"`) {
		t.Fatalf("ordinary collection unexpectedly has path: %s", encoded)
	}

	empty := NewCollectionPaths(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty paths = %#v, want non-nil empty slice", empty)
	}
}

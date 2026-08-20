package zotero

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func pathCollection(key, name, parent string) Envelope {
	return Envelope{
		Key:  key,
		Meta: map[string]json.RawMessage{"numItems": json.RawMessage(`"9"`)},
		Data: json.RawMessage(`{"key":` + strconv.Quote(key) + `,"name":` + strconv.Quote(name) + `,"parentCollection":` + parent + `}`),
	}
}

func TestResolveCollectionPathsPreservesOrderAndDuplicates(t *testing.T) {
	collections := []Envelope{
		pathCollection("LEAF0001", "Methods", `"MID00001"`),
		pathCollection("ROOT0001", "Research", "false"),
		pathCollection("MID00001", "Projects", `"ROOT0001"`),
	}
	paths, err := ResolveCollectionPaths(collections, []string{"LEAF0001", "ROOT0001", "LEAF0001"})
	if err != nil {
		t.Fatalf("ResolveCollectionPaths: %v", err)
	}
	if len(paths) != 3 || paths[0].Key != "LEAF0001" || paths[1].Key != "ROOT0001" || paths[2].Key != "LEAF0001" {
		t.Fatalf("request order/duplicates changed: %#v", paths)
	}
	want := CollectionPath{
		Key: "LEAF0001", Name: "Methods", ParentKey: "MID00001", NumItems: 9,
		Segments: []CollectionPathSegment{
			{Key: "ROOT0001", Name: "Research"},
			{Key: "MID00001", Name: "Projects"},
			{Key: "LEAF0001", Name: "Methods"},
		},
	}
	if !reflect.DeepEqual(paths[0], want) {
		t.Fatalf("leaf path = %#v, want %#v", paths[0], want)
	}
	if got := paths[1]; got.ParentKey != "" || len(got.Segments) != 1 || got.Segments[0].Key != "ROOT0001" {
		t.Fatalf("root path = %#v", got)
	}
}

func TestResolveCollectionPathsRejectsInvalidGraphs(t *testing.T) {
	for _, test := range []struct {
		name        string
		collections []Envelope
		keys        []string
		want        string
	}{
		{name: "empty response key", collections: []Envelope{{Data: json.RawMessage(`{"name":"No key"}`)}}, keys: []string{"ANY00001"}, want: "response has no key"},
		{name: "duplicate response key", collections: []Envelope{pathCollection("DUPL0001", "One", "false"), pathCollection("DUPL0001", "Two", "false")}, keys: []string{"DUPL0001"}, want: `duplicate key "DUPL0001"`},
		{name: "empty requested key", keys: []string{""}, want: "must not be empty"},
		{name: "missing requested", keys: []string{"LOST0001"}, want: `no collection with key "LOST0001"`},
		{name: "missing parent", collections: []Envelope{pathCollection("LEAF0001", "Leaf", `"LOST0001"`)}, keys: []string{"LEAF0001"}, want: `references missing parent "LOST0001"`},
		{name: "cycle", collections: []Envelope{pathCollection("ONE00001", "One", `"TWO00002"`), pathCollection("TWO00002", "Two", `"ONE00001"`)}, keys: []string{"ONE00001"}, want: `cycle at "ONE00001"`},
		{name: "missing data", collections: []Envelope{{Key: "NODATA01"}}, keys: []string{"NODATA01"}, want: "expected a data object"},
		{name: "null data", collections: []Envelope{{Key: "NULLDATA", Data: json.RawMessage(`null`)}}, keys: []string{"NULLDATA"}, want: "expected a data object"},
		{name: "malformed data", collections: []Envelope{{Key: "BADJSON1", Data: json.RawMessage(`{`)}}, keys: []string{"BADJSON1"}, want: `decode collection "BADJSON1"`},
		{name: "missing data key", collections: []Envelope{{Key: "NOKEY001", Data: json.RawMessage(`{"name":"No Key","parentCollection":false}`)}}, keys: []string{"NOKEY001"}, want: "data has no key"},
		{name: "mismatched data key", collections: []Envelope{{Key: "OTHER001", Data: json.RawMessage(`{"key":"TOP00001","name":"Mismatch","parentCollection":false}`)}}, keys: []string{"OTHER001"}, want: `data has key "TOP00001"`},
		{name: "missing name", collections: []Envelope{{Key: "NONAME01", Data: json.RawMessage(`{"key":"NONAME01","parentCollection":false}`)}}, keys: []string{"NONAME01"}, want: "data has no name"},
		{name: "missing parent field", collections: []Envelope{{Key: "BADPAR01", Data: json.RawMessage(`{"key":"BADPAR01","name":"Bad"}`)}}, keys: []string{"BADPAR01"}, want: "non-empty collection key or false"},
		{name: "null parent", collections: []Envelope{pathCollection("BADPAR01", "Bad", "null")}, keys: []string{"BADPAR01"}, want: "non-empty collection key or false"},
		{name: "empty parent", collections: []Envelope{pathCollection("BADPAR01", "Bad", `""`)}, keys: []string{"BADPAR01"}, want: "non-empty collection key or false"},
		{name: "true parent", collections: []Envelope{pathCollection("BADPAR01", "Bad", "true")}, keys: []string{"BADPAR01"}, want: "non-empty collection key or false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveCollectionPaths(test.collections, test.keys)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestResolveCollectionPathsIgnoresUnrelatedMalformedData(t *testing.T) {
	paths, err := ResolveCollectionPaths([]Envelope{
		pathCollection("ROOT0001", "Root", "false"),
		pathCollection("BROKEN01", "Broken", "true"),
	}, []string{"ROOT0001"})
	if err != nil {
		t.Fatalf("ResolveCollectionPaths: %v", err)
	}
	if len(paths) != 1 || paths[0].Key != "ROOT0001" {
		t.Fatalf("paths = %#v", paths)
	}
}

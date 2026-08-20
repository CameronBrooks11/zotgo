package zotero

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeRelationsNormalizesAndSortsTargets(t *testing.T) {
	raw := json.RawMessage(`{
		"key":"SOURCE01",
		"data":{"itemType":"book","relations":{
			"owl:sameAs":"https://example.com/work/1",
			"dc:relation":[
				"http://zotero.org/users/123/items/BBBB2222",
				"http://zotero.org/users/local/localKey/items/LOCAL001",
				"https://www.zotero.org/groups/42/items/AAAA1111",
				"http://zotero.org/users/123/items/BBBB2222"
			]
		}}
	}`)
	got, err := DecodeRelations(raw)
	if err != nil {
		t.Fatalf("DecodeRelations: %v", err)
	}
	want := []Relation{
		{Predicate: "dc:relation", Target: "http://zotero.org/users/123/items/BBBB2222", TargetKey: "BBBB2222"},
		{Predicate: "dc:relation", Target: "http://zotero.org/users/123/items/BBBB2222", TargetKey: "BBBB2222"},
		{Predicate: "dc:relation", Target: "http://zotero.org/users/local/localKey/items/LOCAL001", TargetKey: "LOCAL001"},
		{Predicate: "dc:relation", Target: "https://www.zotero.org/groups/42/items/AAAA1111", TargetKey: "AAAA1111"},
		{Predicate: "owl:sameAs", Target: "https://example.com/work/1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relations = %#v, want %#v", got, want)
	}
}

func TestRelationTargetKeyRecognizesOnlyStrictZoteroItemURIs(t *testing.T) {
	for _, test := range []struct {
		target string
		want   string
	}{
		{target: "http://zotero.org/users/123/items/ABCD1234", want: "ABCD1234"},
		{target: "https://www.zotero.org/groups/42/items/EFGH5678", want: "EFGH5678"},
		{target: "http://zotero.org/users/local/localKey/items/LOCAL001", want: "LOCAL001"},
		{target: "https://example.com/users/123/items/ABCD1234"},
		{target: "https://zotero.org/users/0/items/ABCD1234"},
		{target: "https://zotero.org/users/01/items/ABCD1234"},
		{target: "https://zotero.org/users/123/collections/ABCD1234"},
		{target: "https://zotero.org/users/123/items/ABCD1234/child"},
		{target: "https://zotero.org/users/123/items/ABCD1234?x=1"},
		{target: "https://zotero.org/users/123/items/ABCD1234#fragment"},
		{target: "https://zotero.org:443/users/123/items/ABCD1234"},
		{target: "/users/123/items/ABCD1234"},
	} {
		if got := relationTargetKey(test.target); got != test.want {
			t.Errorf("relationTargetKey(%q) = %q, want %q", test.target, got, test.want)
		}
	}
}

func TestDecodeRelationsEmptyAndMalformed(t *testing.T) {
	for _, relations := range []string{"", `,"relations":null`, `,"relations":{}`} {
		raw := json.RawMessage(`{"key":"SOURCE01","data":{"itemType":"book"` + relations + `}}`)
		got, err := DecodeRelations(raw)
		if err != nil {
			t.Fatalf("relations %q: %v", relations, err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("relations %q = %#v, want non-nil empty", relations, got)
		}
	}

	for _, test := range []struct {
		name      string
		relations string
		want      string
	}{
		{name: "not object", relations: `[]`, want: "JSON object"},
		{name: "null target", relations: `{"dc:relation":null}`, want: "string or array"},
		{name: "numeric target", relations: `{"dc:relation":42}`, want: "string or array"},
		{name: "mixed array", relations: `{"dc:relation":["ok",42]}`, want: "cannot unmarshal"},
		{name: "empty target", relations: `{"dc:relation":""}`, want: "must not be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"key":"SOURCE01","data":{"itemType":"book","relations":` + test.relations + `}}`)
			_, err := DecodeRelations(raw)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

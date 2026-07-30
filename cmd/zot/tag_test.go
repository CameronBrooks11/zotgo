package main

import (
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestAddTags(t *testing.T) {
	existing := []zotero.Tag{{Tag: "a"}, {Tag: "b", Type: 1}}
	next, added := addTags(existing, []string{"a", "c"})
	if len(next) != 3 || next[2].Tag != "c" {
		t.Errorf("next = %+v, want a,b,c", next)
	}
	if strings.Join(added, ",") != "c" {
		t.Errorf("added = %v, want [c] (a already present)", added)
	}
	// automatic tag's type is preserved through the splice
	if next[1].Type != 1 {
		t.Errorf("existing automatic tag lost its type: %+v", next[1])
	}
}

func TestRemoveTags(t *testing.T) {
	existing := []zotero.Tag{{Tag: "a"}, {Tag: "b"}, {Tag: "c"}}
	next, removed := removeTags(existing, []string{"b", "z"})
	if len(next) != 2 || next[0].Tag != "a" || next[1].Tag != "c" {
		t.Errorf("next = %+v, want a,c", next)
	}
	if strings.Join(removed, ",") != "b" {
		t.Errorf("removed = %v, want [b] (z absent)", removed)
	}
}

func TestTagsPatch(t *testing.T) {
	empty, err := tagsPatch(nil)
	if err != nil || string(empty) != `{"tags":[]}` {
		t.Errorf("empty patch = %s (%v), want {\"tags\":[]}", empty, err)
	}
	one, err := tagsPatch([]zotero.Tag{{Tag: "x"}})
	if err != nil || !strings.Contains(string(one), `"tag":"x"`) {
		t.Errorf("patch = %s", one)
	}
}

func TestCapitalize(t *testing.T) {
	if capitalize("add") != "Add" || capitalize("remove") != "Remove" || capitalize("") != "" {
		t.Error("capitalize failed")
	}
}

package output

import (
	"encoding/json"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// These tests pin the JSON shape of DTO constructors that were previously
// exercised only transitively through cmd tests. The DTOs are a versioned
// contract, so a field rename or reshape must break a test here, not slip
// through a shallow kind-only assertion.

func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestNewStatsShape(t *testing.T) {
	m := marshalToMap(t, NewStats(zotero.Stats{Items: 42, TopItems: 30, Collections: 5, Tags: 7}))
	for key, want := range map[string]float64{"items": 42, "topItems": 30, "collections": 5, "tags": 7} {
		if got, ok := m[key]; !ok || got != want {
			t.Errorf("stats[%q] = %v (present=%v), want %v", key, got, ok, want)
		}
	}
}

func TestNewRelationsShape(t *testing.T) {
	rels := NewRelations("SRC00001", []zotero.Relation{
		{Predicate: "dc:relation", Target: "http://zotero.org/users/0/items/TGT00001", TargetKey: "TGT00001"},
	})
	if len(rels) != 1 {
		t.Fatalf("len = %d, want 1", len(rels))
	}
	m := marshalToMap(t, rels[0])
	for key, want := range map[string]string{
		"itemKey":   "SRC00001",
		"predicate": "dc:relation",
		"targetKey": "TGT00001",
	} {
		if got, _ := m[key].(string); got != want {
			t.Errorf("relation[%q] = %q, want %q", key, got, want)
		}
	}
	if _, ok := m["target"]; !ok {
		t.Error("relation missing target field")
	}
}

func TestNewLibraryFilesShape(t *testing.T) {
	if NewLibraryFiles(nil) != nil {
		t.Error("nil input must yield nil so the Health envelope omits the field")
	}
	libs := NewLibraryFiles([]zotero.LibraryFiles{{ID: 6, Name: "Reactor", FilesEditable: false}})
	m := marshalToMap(t, libs[0])
	if m["id"].(float64) != 6 || m["name"].(string) != "Reactor" {
		t.Errorf("library file record = %v", m)
	}
	if files, ok := m["filesEditable"].(bool); !ok || files {
		t.Errorf("filesEditable = %v, want false", m["filesEditable"])
	}
}

func TestNewHealthLocalShape(t *testing.T) {
	h := zotero.Health{
		Endpoint:        zotero.LocalProfile("http://localhost:23119"),
		Reachable:       true,
		ZoteroRunning:   true,
		LocalAPIEnabled: true,
	}
	m := marshalToMap(t, NewHealth(h))

	endpoint := m["endpoint"].(map[string]any)
	if endpoint["kind"] != "local" {
		t.Errorf("endpoint.kind = %v, want local", endpoint["kind"])
	}
	if _, ok := m["zoteroRunning"]; !ok {
		t.Error("local health must carry zoteroRunning")
	}
	if _, ok := m["localApiEnabled"]; !ok {
		t.Error("local health must carry localApiEnabled")
	}
	if _, ok := m["keyValid"]; ok {
		t.Error("local health must not carry the web-only keyValid")
	}
	if _, ok := m["capabilities"]; !ok {
		t.Error("health must always list capabilities")
	}
}

func TestNewHealthWebShape(t *testing.T) {
	h := zotero.Health{
		Endpoint:  zotero.WebProfile("https://api.zotero.org"),
		Reachable: true,
		KeyValid:  true,
		WebUserID: 12345,
	}
	m := marshalToMap(t, NewHealth(h))

	if m["endpoint"].(map[string]any)["kind"] != "web" {
		t.Errorf("endpoint.kind = %v, want web", m["endpoint"])
	}
	if _, ok := m["keyValid"]; !ok {
		t.Error("web health must carry keyValid")
	}
	if m["userId"].(float64) != 12345 {
		t.Errorf("userId = %v, want 12345", m["userId"])
	}
	if _, ok := m["zoteroRunning"]; ok {
		t.Error("web health must not carry the local-only zoteroRunning")
	}
}

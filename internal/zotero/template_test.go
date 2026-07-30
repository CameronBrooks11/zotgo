package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestItemTemplate_AssemblesFromEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/itemTypeFields", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("itemType") != "book" {
			http.Error(w, "Invalid or missing 'itemType' value", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"field":"title","localized":"Title"},{"field":"edition","localized":"Edition"}]`))
	})
	mux.HandleFunc("/api/itemTypeCreatorTypes", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"creatorType":"author"},{"creatorType":"editor"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tpl, err := New(srv.URL).ItemTemplate(context.Background(), "book")
	if err != nil {
		t.Fatalf("ItemTemplate: %v", err)
	}
	// Ordered: itemType must come before the fields.
	s := string(tpl)
	if strings.Index(s, `"itemType"`) > strings.Index(s, `"title"`) {
		t.Errorf("itemType should precede fields:\n%s", s)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(tpl, &m); err != nil {
		t.Fatalf("template is not valid JSON: %v", err)
	}
	for _, k := range []string{"itemType", "title", "edition", "creators", "tags", "collections", "relations"} {
		if _, ok := m[k]; !ok {
			t.Errorf("template missing %q", k)
		}
	}
	// The primary creator type seeds one blank creator.
	if !strings.Contains(string(m["creators"]), `"author"`) {
		t.Errorf("creators = %s, want a blank author", m["creators"])
	}
}

func TestItemTemplate_UnknownTypeIsClear(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Invalid or missing 'itemType' value", http.StatusBadRequest)
	}))
	defer srv.Close()
	_, err := New(srv.URL).ItemTemplate(context.Background(), "notARealType")
	if err == nil || !strings.Contains(err.Error(), "unknown item type") {
		t.Fatalf("err = %v, want an 'unknown item type' message", err)
	}
}

package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExport_BibtexConcatenatesPagesByteExact(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items/top", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "bibtex" {
			t.Errorf("format = %q, want bibtex", r.URL.Query().Get("format"))
		}
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", "<"+srv.URL+"/api/users/0/items/top?format=bibtex&start=1>; rel=\"next\"")
			_, _ = w.Write([]byte("@article{a2009,\n\ttitle = {A}\n}\n"))
		case "1":
			_, _ = w.Write([]byte("@book{b2010,\n\ttitle = {B}\n}\n"))
		default:
			t.Fatalf("unexpected start %q", r.URL.Query().Get("start"))
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	got, err := New(srv.URL).Export(context.Background(), UserLibrary(), ItemsOptions{Top: true}, "bibtex")
	if err != nil {
		t.Fatalf("Export bibtex: %v", err)
	}
	want := "@article{a2009,\n\ttitle = {A}\n}\n\n@book{b2010,\n\ttitle = {B}\n}\n"
	if string(got) != want {
		t.Fatalf("bibtex mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestExport_CSLJSONMergesPages(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items/top", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", "<"+srv.URL+"/api/users/0/items/top?format=csljson&start=1>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"id":"a","type":"article-journal"}]`))
		case "1":
			_, _ = w.Write([]byte(`[{"id":"b","type":"book"}]`))
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	got, err := New(srv.URL).Export(context.Background(), UserLibrary(), ItemsOptions{Top: true}, "csljson")
	if err != nil {
		t.Fatalf("Export csljson: %v", err)
	}
	var merged []map[string]any
	if err := json.Unmarshal(got, &merged); err != nil {
		t.Fatalf("merged output not valid JSON: %v\n%s", err, got)
	}
	if len(merged) != 2 || merged[0]["id"] != "a" || merged[1]["id"] != "b" {
		t.Fatalf("merged = %v", merged)
	}
}

// The Web API wraps csljson pages as {"items":[…]} while the Local API returns a
// bare array; both must unwrap to one flat array so the output is endpoint-neutral.
func TestMergeCSLJSON_UnwrapsBothPageShapes(t *testing.T) {
	pages := [][]byte{
		[]byte(`{"items":[{"id":"a"},{"id":"b"}]}`), // Web API envelope
		[]byte(`[{"id":"c"}]`),                      // Local API bare array
	}
	got, err := mergeCSLJSON("csljson", pages)
	if err != nil {
		t.Fatalf("mergeCSLJSON: %v", err)
	}
	var merged []map[string]any
	if err := json.Unmarshal(got, &merged); err != nil {
		t.Fatalf("merged output not a bare array: %v\n%s", err, got)
	}
	ids := []string{}
	for _, m := range merged {
		ids = append(ids, m["id"].(string))
	}
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("merged ids = %v, want [a b c]", ids)
	}
}

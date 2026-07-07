package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExportBibtex_ConcatenatesPagesByteExact(t *testing.T) {
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

	got, err := New(srv.URL).ExportBibtex(context.Background(), UserLibrary(), ItemsOptions{Top: true})
	if err != nil {
		t.Fatalf("ExportBibtex: %v", err)
	}
	want := "@article{a2009,\n\ttitle = {A}\n}\n\n@book{b2010,\n\ttitle = {B}\n}\n"
	if string(got) != want {
		t.Fatalf("bibtex mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestExportCSLJSON_MergesPages(t *testing.T) {
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

	got, err := New(srv.URL).ExportCSLJSON(context.Background(), UserLibrary(), ItemsOptions{Top: true})
	if err != nil {
		t.Fatalf("ExportCSLJSON: %v", err)
	}
	var merged []map[string]any
	if err := json.Unmarshal(got, &merged); err != nil {
		t.Fatalf("merged output not valid JSON: %v\n%s", err, got)
	}
	if len(merged) != 2 || merged[0]["id"] != "a" || merged[1]["id"] != "b" {
		t.Fatalf("merged = %v", merged)
	}
}

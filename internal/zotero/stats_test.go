package zotero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStats_CountsFromHeaders(t *testing.T) {
	totals := map[string]string{
		"/api/users/0/items":       "2203",
		"/api/users/0/items/top":   "1093",
		"/api/users/0/collections": "57",
		"/api/users/0/tags":        "812",
	}
	mux := http.NewServeMux()
	for path, total := range totals {
		total := total
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("limit") != "1" {
				t.Errorf("%s: limit = %q, want 1", r.URL.Path, r.URL.Query().Get("limit"))
			}
			w.Header().Set("Total-Results", total)
			_, _ = w.Write([]byte("[]"))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := New(srv.URL).Stats(context.Background(), UserLibrary())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Items != 2203 || s.TopItems != 1093 || s.Collections != 57 || s.Tags != 812 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestAllCollections_FollowsNextLink(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/collections", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", "<"+srv.URL+"/api/users/0/collections?start=1>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"key":"AAAA","data":{"key":"AAAA","name":"One","parentCollection":false}}]`))
		case "1":
			_, _ = w.Write([]byte(`[{"key":"BBBB","data":{"key":"BBBB","name":"Two","parentCollection":"AAAA"}}]`))
		default:
			t.Fatalf("unexpected start %q", r.URL.Query().Get("start"))
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	cols, err := New(srv.URL).AllCollections(context.Background(), UserLibrary(), CollectionsOptions{})
	if err != nil {
		t.Fatalf("AllCollections: %v", err)
	}
	if len(cols) != 2 || cols[0].Key != "AAAA" || cols[1].Key != "BBBB" {
		t.Fatalf("cols = %+v", cols)
	}
	if data, _ := cols[1].CollectionData(); data.ParentKey() != "AAAA" {
		t.Errorf("parent key = %q, want AAAA", data.ParentKey())
	}
}

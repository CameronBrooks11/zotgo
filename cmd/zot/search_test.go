package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSearchZotero serves the items endpoint the search command queries, echoing
// enough to prove the query and type filter were routed.
func fakeSearchZotero(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		_, _ = w.Write([]byte("Zotero is running"))
	})
	mux.HandleFunc("GET /api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Errorf("search did not pass a query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Total-Results", "1")
		_, _ = w.Write([]byte(`[{"key":"SRCH0001","data":{"key":"SRCH0001","itemType":"journalArticle","title":"Found paper"}}]`))
	})
	return httptest.NewServer(mux)
}

// The query is validated before any network call, so an empty one fails fast.
func TestSearchEmptyQueryErrors(t *testing.T) {
	_, _, err := runCLI("http://unused.invalid", "search")
	if err == nil || !strings.Contains(err.Error(), "missing search query") {
		t.Fatalf("err = %v, want a missing-query message", err)
	}
}

func TestSearchTable(t *testing.T) {
	srv := fakeSearchZotero(t)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "search", "state", "estimation")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"SRCH0001", "Found paper"} {
		if !strings.Contains(out, want) {
			t.Errorf("search output missing %q:\n%s", want, out)
		}
	}
}

func TestSearchJSON(t *testing.T) {
	srv := fakeSearchZotero(t)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "search", "algae")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var doc struct {
		Kind string `json:"kind"`
		Data []struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if doc.Kind != "items" || len(doc.Data) != 1 || doc.Data[0].Key != "SRCH0001" {
		t.Fatalf("doc = %+v", doc)
	}
}

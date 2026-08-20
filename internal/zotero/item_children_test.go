package zotero

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRawItemChildrenPaginatesAndPreservesEnvelopes(t *testing.T) {
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items/PARENT01/children?start=1>; rel="next"`, base))
			w.Header().Set("Backoff", "1")
			_, _ = w.Write([]byte(`[ {"future":true,"key":"CHILD01","version":"","data":{"itemType":"attachment","title":"First"}} ]`))
		case "1":
			_, _ = w.Write([]byte(`[{"key":"CHILD02","data":{"itemType":"note","note":"<p>Second</p>","number":1e3}}]`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	client := New(srv.URL)
	recorder := &sleepRecorder{}
	client.sleep = recorder.sleep
	raw, err := client.allRawItemChildren(context.Background(), UserLibrary(), "PARENT01")
	if err != nil {
		t.Fatalf("allRawItemChildren: %v", err)
	}
	if len(raw) != 2 || !strings.Contains(string(raw[0]), `"version":""`) || !strings.Contains(string(raw[1]), `"number":1e3`) {
		t.Fatalf("raw children = %q", raw)
	}
	if len(recorder.waits) != 1 || recorder.waits[0] != time.Second {
		t.Fatalf("backoff waits = %v", recorder.waits)
	}
}

func TestRawItemWithChildrenHonorsParentBackoff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Backoff", "2")
		_, _ = w.Write([]byte(`{"key":"PARENT01","data":{"itemType":"book"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL)
	recorder := &sleepRecorder{}
	client.sleep = recorder.sleep
	item, children, err := client.RawItemWithChildren(context.Background(), UserLibrary(), "PARENT01")
	if err != nil {
		t.Fatalf("RawItemWithChildren: %v", err)
	}
	if item == nil || len(children) != 0 {
		t.Fatalf("item/children = %q/%#v", item, children)
	}
	if len(recorder.waits) != 1 || recorder.waits[0] != 2*time.Second {
		t.Fatalf("backoff waits = %v", recorder.waits)
	}
}

func TestRawItemChildrenRejectsInvalidPages(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "null", body: "null"},
		{name: "object", body: `{}`},
		{name: "malformed", body: `[{`},
		{name: "missing key", body: `[{"data":{"itemType":"note"}}]`},
		{name: "missing item type", body: `[{"key":"CHILD01","data":{}}]`},
		{name: "repeats parent", body: `[{"key":"PARENT01","data":{"itemType":"note"}}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			children, err := New(server.URL).allRawItemChildren(context.Background(), UserLibrary(), "PARENT01")
			if err == nil {
				t.Fatal("expected an error")
			}
			if children != nil {
				t.Fatalf("children after error = %#v", children)
			}
		})
	}
}

func TestRawItemChildrenDiscardsEarlierPagesOnFailure(t *testing.T) {
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items/PARENT01/children?start=1>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"key":"CHILD01","data":{"itemType":"note"}}]`))
			return
		}
		_, _ = w.Write([]byte(`{"not":"an array"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	children, err := New(srv.URL).allRawItemChildren(context.Background(), UserLibrary(), "PARENT01")
	if err == nil {
		t.Fatal("expected a later-page error")
	}
	if children != nil {
		t.Fatalf("children after later-page error = %#v", children)
	}
}

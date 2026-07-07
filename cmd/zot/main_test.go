package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// fakeZotero serves the Local API and connector routes the read commands use.
func fakeZotero(localAPIEnabled bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		_, _ = w.Write([]byte("Zotero is running"))
	})

	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !localAPIEnabled {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Local API is not enabled"))
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("GET /api/users/0/items/top", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "2")
		w.Header().Set("Zotero-Schema-Version", "42")
		_, _ = w.Write([]byte(`[
			{"key":"AAAA1111","data":{"key":"AAAA1111","itemType":"journalArticle","title":"Algae paper"},"meta":{"creatorSummary":"Posten","parsedDate":"2009"}},
			{"key":"BBBB2222","data":{"key":"BBBB2222","itemType":"book","title":"A Book"},"meta":{"creatorSummary":"Author"}}
		]`))
	}))
	mux.HandleFunc("GET /api/users/0/items/{key}/children", guard(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"CHILD001","data":{"itemType":"attachment","title":"Full Text PDF"}}]`))
	}))
	mux.HandleFunc("GET /api/users/0/items/{key}", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") != "AAAA1111" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"key":"AAAA1111","data":{"key":"AAAA1111","itemType":"journalArticle","title":"Algae paper","tags":[{"tag":"ml"}]},"meta":{"creatorSummary":"Posten","parsedDate":"2009"}}`))
	}))
	mux.HandleFunc("GET /api/users/0/items", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "42")
		_, _ = w.Write([]byte("[]"))
	}))
	mux.HandleFunc("GET /api/users/0/collections/{key}/items/top", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") != "ROOT0001" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Total-Results", "1")
		_, _ = w.Write([]byte(`[{"key":"INCOL001","data":{"key":"INCOL001","itemType":"book","title":"In Research"}}]`))
	}))
	mux.HandleFunc("GET /api/users/0/collections", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "2")
		_, _ = w.Write([]byte(`[
			{"key":"ROOT0001","data":{"key":"ROOT0001","name":"Research","parentCollection":false}},
			{"key":"CHILD001","data":{"key":"CHILD001","name":"Subtopic","parentCollection":"ROOT0001"}}
		]`))
	}))
	mux.HandleFunc("GET /api/users/0/tags", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "7")
		_, _ = w.Write([]byte("[]"))
	}))
	return httptest.NewServer(mux)
}

func runCLI(url string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root := rootCommand()
	root.Writer = &stdout
	root.ErrWriter = &stderr
	full := append([]string{"zot", "--url", url}, args...)
	err := root.Run(context.Background(), full)
	return stdout.String(), stderr.String(), err
}

func TestDoctorReady(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "doctor")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Ready") {
		t.Errorf("output missing Ready:\n%s", out)
	}
}

func TestDoctorDisabledExitsNonZero(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "doctor")
	var coder cli.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 1 {
		t.Fatalf("err = %v, want ExitCoder(1)", err)
	}
	if !strings.Contains(out, "Local API disabled") {
		t.Errorf("output missing disabled guidance:\n%s", out)
	}
}

func TestListTable(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"AAAA1111", "Algae paper", "journalArticle", "2 items"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestListJSON(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
}

func TestShowNotFound(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "show", "ZZZZ9999")
	if err == nil || !strings.Contains(err.Error(), "no item with key") {
		t.Fatalf("err = %v, want not-found message", err)
	}
}

func TestShowRendersChildren(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "show", "AAAA1111")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"Algae paper", "Children (1)", "Full Text PDF"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestStats(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "stats")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"My Library", "42", "7"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats output missing %q:\n%s", want, out)
		}
	}
}

func TestCollectionsTree(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "collections")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Research") || !strings.Contains(out, "Subtopic") {
		t.Fatalf("tree missing nodes:\n%s", out)
	}
}

func TestListByCollectionName(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	// "Research" must resolve to key ROOT0001 and route to its items.
	out, _, err := runCLI(srv.URL, "list", "-c", "Research")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "In Research") || !strings.Contains(out, "INCOL001") {
		t.Errorf("collection-name routing failed:\n%s", out)
	}
}

func TestListUnknownCollection(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "list", "-c", "Nonexistent")
	if err == nil || !strings.Contains(err.Error(), "no collection matching") {
		t.Fatalf("err = %v, want no-collection message", err)
	}
}

func TestZoteroDownFriendlyMessage(t *testing.T) {
	srv := fakeZotero(true)
	url := srv.URL
	srv.Close()
	_, _, err := runCLI(url, "list")
	if err == nil || !strings.Contains(err.Error(), "cannot reach Zotero") {
		t.Fatalf("err = %v, want friendly down message", err)
	}
}

func TestLocalAPIDisabledFriendlyMessage(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "stats")
	if err == nil || !strings.Contains(err.Error(), "Local API is disabled") {
		t.Fatalf("err = %v, want friendly disabled message", err)
	}
}

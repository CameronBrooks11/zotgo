package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	showParentBody = ` {"futureTop":true,"key":"PARENT01","version":"","data":{"key":"PARENT01","version":"","itemType":"journalArticle","title":"Parent","futureData":"\u0061"}} `
	showChildOne   = `{"futureChild":1,"key":"CHILD01","version":"","data":{"key":"CHILD01","version":"","itemType":"attachment","title":"PDF"}}`
	showChildTwo   = `{"key":"CHILD02","data":{"key":"CHILD02","itemType":"note","title":"Note","number":1e3}}`
)

func pagedShowServer(t *testing.T, badSecondPage bool) *httptest.Server {
	t.Helper()
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(showParentBody))
	})
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items/PARENT01/children?start=1>; rel="next"`, base))
			_, _ = w.Write([]byte(`[ ` + showChildOne + ` ]`))
			return
		}
		if badSecondPage {
			_, _ = w.Write([]byte(`{"not":"an array"}`))
			return
		}
		_, _ = w.Write([]byte(`[` + showChildTwo + `]`))
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	return srv
}

func TestShowReadsAllChildrenInEveryMode(t *testing.T) {
	srv := pagedShowServer(t, false)
	defer srv.Close()

	human, _, err := runCLI(srv.URL, "show", "PARENT01")
	if err != nil {
		t.Fatalf("human show: %v", err)
	}
	for _, want := range []string{"PARENT01", "CHILD01", "CHILD02", "PDF", "Note"} {
		if !strings.Contains(human, want) {
			t.Errorf("human output missing %q:\n%s", want, human)
		}
	}

	for _, mode := range []string{"--json", "--jsonl"} {
		out, _, err := runCLI(srv.URL, mode, "show", "PARENT01")
		if err != nil {
			t.Fatalf("%s show: %v", mode, err)
		}
		if mode == "--jsonl" && strings.Count(strings.TrimSpace(out), "\n") != 0 {
			t.Fatalf("JSONL has multiple lines:\n%s", out)
		}
		var document struct {
			Kind string `json:"kind"`
			Data struct {
				Key      string `json:"key"`
				Children []struct {
					Key string `json:"key"`
				} `json:"children"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &document); err != nil {
			t.Fatalf("decode %s: %v\n%s", mode, err, out)
		}
		if document.Kind != "item" || document.Data.Key != "PARENT01" || len(document.Data.Children) != 2 || document.Data.Children[0].Key != "CHILD01" || document.Data.Children[1].Key != "CHILD02" {
			t.Fatalf("%s document = %#v", mode, document)
		}
	}

	raw, _, err := runCLI(srv.URL, "--raw", "show", "PARENT01")
	if err != nil {
		t.Fatalf("raw show: %v", err)
	}
	compact := `{"item":` + strings.TrimSpace(showParentBody) + `,"children":[` + showChildOne + `,` + showChildTwo + `]}`
	wantRaw := indentRaw(t, compact)
	if raw != wantRaw {
		t.Fatalf("raw output changed envelopes:\n got: %q\nwant: %q", raw, wantRaw)
	}
	// Fidelity: the embedded envelopes' exact tokens survive indentation — the
	// \u0061 escape is not unescaped and 1e3 is not re-encoded to 1000.
	for _, token := range []string{`\u0061`, `1e3`} {
		if !strings.Contains(raw, token) {
			t.Errorf("raw output lost verbatim token %q:\n%s", token, raw)
		}
	}
}

// indentRaw mirrors rawShowDocument's 2-space formatting, so a test can assert
// the pretty-printed shape without hand-maintaining an indented string literal.
func indentRaw(t *testing.T, compact string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(compact), "", "  "); err != nil {
		t.Fatalf("indent %q: %v", compact, err)
	}
	buf.WriteByte('\n')
	return buf.String()
}

func TestShowEmitsEmptyChildrenArray(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"PARENT01","data":{"key":"PARENT01","itemType":"book","title":"Parent"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	stable, _, err := runCLI(srv.URL, "--json", "show", "PARENT01")
	if err != nil {
		t.Fatalf("stable show: %v", err)
	}
	if !strings.Contains(stable, `"children": []`) {
		t.Fatalf("stable output has no empty children array:\n%s", stable)
	}
	raw, _, err := runCLI(srv.URL, "--raw", "show", "PARENT01")
	if err != nil {
		t.Fatalf("raw show: %v", err)
	}
	wantEmpty := indentRaw(t, `{"item":{"key":"PARENT01","data":{"key":"PARENT01","itemType":"book","title":"Parent"}},"children":[]}`)
	if raw != wantEmpty {
		t.Fatalf("raw empty children:\n got: %q\nwant: %q", raw, wantEmpty)
	}
}

func TestShowBuffersBeforeLaterPageFailure(t *testing.T) {
	for _, args := range [][]string{
		{"show", "PARENT01"},
		{"--json", "show", "PARENT01"},
		{"--jsonl", "show", "PARENT01"},
		{"--raw", "show", "PARENT01"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			srv := pagedShowServer(t, true)
			defer srv.Close()
			stdout, _, err := runCLI(srv.URL, args...)
			if err == nil {
				t.Fatal("expected later-page error")
			}
			if stdout != "" {
				t.Fatalf("stdout after later-page error = %q", stdout)
			}
		})
	}
}

func TestShowRawWebProfilePaginates(t *testing.T) {
	var base string
	var paths []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"userID":12345,"username":"ada","access":{"user":{"library":true,"write":false}}}`))
	})
	mux.HandleFunc("GET /users/12345/items/PARENT01", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_, _ = w.Write([]byte(showParentBody))
	})
	mux.HandleFunc("GET /users/12345/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.Header.Get("Zotero-API-Key") != "test-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("start") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/users/12345/items/PARENT01/children?start=1>; rel="next"`, base))
			_, _ = w.Write([]byte(`[` + showChildOne + `]`))
			return
		}
		_, _ = w.Write([]byte(`[` + showChildTwo + `]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	t.Setenv("ZOTGO_API_KEY", "test-key")

	out, _, err := runCLI(srv.URL, "--web", "--raw", "show", "PARENT01")
	if err != nil {
		t.Fatalf("web raw show: %v", err)
	}
	if !strings.Contains(out, "CHILD02") {
		t.Fatalf("web raw output missing second page:\n%s", out)
	}
	joined := strings.Join(paths, ",")
	if strings.Contains(joined, "/api/") || !strings.Contains(joined, "/users/12345/items/PARENT01/children?start=1") {
		t.Fatalf("web paths = %s", joined)
	}
}

func TestShowHelpDocumentsStableAndRawPaths(t *testing.T) {
	out, _, err := runCLI("http://unused.invalid", "show", "--help")
	if err != nil {
		t.Fatalf("show help: %v", err)
	}
	for _, want := range []string{"all of its children", ".data.children", ".item", ".children"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

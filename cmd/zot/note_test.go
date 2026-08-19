package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const noteEnvelope = `{
	"key":"NOTE0001",
	"version":"",
	"futureTop":{"kept":true},
	"data":{
		"itemType":"note",
		"parentItem":"PARENT01",
		"dateAdded":"2026-01-01T00:00:00Z",
		"dateModified":"2026-01-02T00:00:00Z",
		"tags":[{"tag":"review","type":0}],
		"note":"<div data-schema-version=\"9\"><p>EXACT BODY</p></div>",
		"futureData":7
	}
}`

func noteServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("key") {
		case "NOTE0001":
			_, _ = w.Write([]byte(noteEnvelope))
		case "EMPTY001":
			_, _ = w.Write([]byte(`{"key":"EMPTY001","data":{"itemType":"note","parentItem":false,"tags":[],"note":""}}`))
		case "BROKEN01":
			_, _ = w.Write([]byte(`{"key":"BROKEN01","futureTop":true,"data":{"itemType":"note","note":null,"futureData":7}}`))
		case "WRONG001":
			_, _ = w.Write([]byte(`{"key":"WRONG001","data":{"itemType":"book","title":"Not a note"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return httptest.NewServer(mux)
}

func TestNoteShowAllOutputModes(t *testing.T) {
	srv := noteServer()
	defer srv.Close()

	help, _, err := runCLI(srv.URL, "note", "show", "--help")
	if err != nil {
		t.Fatalf("note help: %v", err)
	}
	for _, want := range []string{"<note-key>", "rich-text HTML", "html field", "--raw"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}

	human, _, err := runCLI(srv.URL, "note", "show", "NOTE0001")
	if err != nil {
		t.Fatalf("human note show: %v", err)
	}
	for _, want := range []string{"NOTE0001", "PARENT01", "2026-01-01", "2026-01-02", "review", "HTML:", `<div data-schema-version="9"><p>EXACT BODY</p></div>`} {
		if !strings.Contains(human, want) {
			t.Errorf("human output missing %q:\n%s", want, human)
		}
	}

	for _, mode := range []string{"--json", "--jsonl"} {
		out, _, err := runCLI(srv.URL, mode, "note", "show", "NOTE0001")
		if err != nil {
			t.Fatalf("%s note show: %v", mode, err)
		}
		if mode == "--jsonl" && strings.Count(strings.TrimSpace(out), "\n") != 0 {
			t.Fatalf("JSONL has multiple lines: %s", out)
		}
		var document struct {
			Schema int            `json:"schema"`
			Kind   string         `json:"kind"`
			Data   map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &document); err != nil {
			t.Fatalf("decode %s: %v\n%s", mode, err, out)
		}
		if document.Schema != 2 || document.Kind != "note" || document.Data["key"] != "NOTE0001" || document.Data["parentKey"] != "PARENT01" || document.Data["html"] != `<div data-schema-version="9"><p>EXACT BODY</p></div>` {
			t.Fatalf("%s document = %#v", mode, document)
		}
		if len(document.Data) != 6 || strings.Contains(out, `"version"`) || strings.Contains(out, "futureTop") || strings.Contains(out, "futureData") {
			t.Fatalf("%s stable fields changed: %s", mode, out)
		}
	}

	raw, _, err := runCLI(srv.URL, "--raw", "note", "show", "NOTE0001")
	if err != nil {
		t.Fatalf("raw note show: %v", err)
	}
	for _, want := range []string{`"version": ""`, `"futureTop"`, `"futureData"`, "EXACT BODY"} {
		if !strings.Contains(raw, want) {
			t.Errorf("raw output missing %q:\n%s", want, raw)
		}
	}
}

func TestNoteShowEmptyMalformedAndInputErrors(t *testing.T) {
	srv := noteServer()
	defer srv.Close()

	empty, _, err := runCLI(srv.URL, "--json", "note", "show", "EMPTY001")
	if err != nil {
		t.Fatalf("empty note: %v", err)
	}
	if !strings.Contains(empty, `"parentKey": ""`) || !strings.Contains(empty, `"tags": []`) || !strings.Contains(empty, `"html": ""`) {
		t.Fatalf("empty note fields: %s", empty)
	}

	if raw, _, err := runCLI(srv.URL, "--raw", "note", "show", "BROKEN01"); err != nil || !strings.Contains(raw, `"note": null`) {
		t.Fatalf("raw malformed note = %q, %v", raw, err)
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing argument", args: []string{"note", "show"}, want: "missing note key"},
		{name: "not found", args: []string{"note", "show", "MISSING1"}, want: `no note with key "MISSING1"`},
		{name: "wrong type", args: []string{"note", "show", "WRONG001"}, want: `type "book", not note`},
		{name: "malformed stable body", args: []string{"--json", "note", "show", "BROKEN01"}, want: "note must be a string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, _, err := runCLI(srv.URL, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
			if stdout != "" {
				t.Fatalf("stdout after error = %q", stdout)
			}
		})
	}
}

func TestNoteShowWebProfile(t *testing.T) {
	t.Setenv("ZOTGO_API_KEY", "test-key")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Zotero-API-Key") != "test-key" {
			t.Error("missing API key")
		}
		_, _ = w.Write([]byte(`{"userID":123,"username":"ada","access":{"user":{"library":true}}}`))
	})
	mux.HandleFunc("GET /users/123/items/NOTE0001", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Zotero-API-Key") != "test-key" {
			t.Error("missing API key")
		}
		_, _ = w.Write([]byte(noteEnvelope))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, _, err := runCLI(srv.URL, "--web", "--json", "note", "show", "NOTE0001")
	if err != nil {
		t.Fatalf("web note show: %v", err)
	}
	if !strings.Contains(out, `"id": 123`) || !strings.Contains(out, "EXACT BODY") {
		t.Fatalf("web output: %s", out)
	}
}

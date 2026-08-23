package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const relationEnvelope = `{
	"key":"SOURCE01",
	"version":"",
	"futureTop":{"kept":true},
	"data":{
		"key":"SOURCE01",
		"itemType":"journalArticle",
		"relations":{
			"owl:sameAs":"https://example.com/work/1",
			"dc:relation":[
				"http://zotero.org/users/123/items/TARGET02",
				"http://zotero.org/groups/42/items/TARGET01"
			]
		},
		"futureData":7
	}
}`

func relationServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("key") {
		case "SOURCE01":
			_, _ = w.Write([]byte(relationEnvelope))
		case "EMPTY001":
			_, _ = w.Write([]byte(`{"key":"EMPTY001","data":{"itemType":"book","relations":{}}}`))
		case "BROKEN01":
			_, _ = w.Write([]byte(`{"key":"BROKEN01","futureTop":true,"data":{"itemType":"book","relations":{"dc:relation":42}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return httptest.NewServer(mux)
}

func TestRelationListAllOutputModes(t *testing.T) {
	srv := relationServer()
	defer srv.Close()

	help, _, err := runCLI(srv.URL, "relation", "list", "--help")
	if err != nil {
		t.Fatalf("relation help: %v", err)
	}
	for _, want := range []string{"<item-key>", "outgoing relations", "authoritative", "targetKey", "--raw"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}

	human, _, err := runCLI(srv.URL, "relation", "list", "SOURCE01")
	if err != nil {
		t.Fatalf("human relation list: %v", err)
	}
	for _, want := range []string{"PREDICATE", "TARGET KEY", "TARGET", "dc:relation", "TARGET01", "TARGET02", "owl:sameAs", "https://example.com/work/1"} {
		if !strings.Contains(human, want) {
			t.Errorf("human output missing %q:\n%s", want, human)
		}
	}
	if strings.Index(human, "TARGET01") > strings.Index(human, "TARGET02") {
		t.Fatalf("human output is not stable-sorted:\n%s", human)
	}

	jsonOutput, _, err := runCLI(srv.URL, "--json", "relation", "list", "SOURCE01")
	if err != nil {
		t.Fatalf("JSON relation list: %v", err)
	}
	var document struct {
		Schema int    `json:"schema"`
		Kind   string `json:"kind"`
		Data   []struct {
			ItemKey   string `json:"itemKey"`
			Predicate string `json:"predicate"`
			Target    string `json:"target"`
			TargetKey string `json:"targetKey"`
		} `json:"data"`
		Meta struct {
			Shown int `json:"shown"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &document); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, jsonOutput)
	}
	if document.Schema != 3 || document.Kind != "relations" || document.Meta.Shown != 3 || document.Meta.Total != 3 {
		t.Fatalf("document = %#v", document)
	}
	if len(document.Data) != 3 || document.Data[0].ItemKey != "SOURCE01" || document.Data[0].TargetKey != "TARGET01" || document.Data[2].TargetKey != "" {
		t.Fatalf("records = %#v", document.Data)
	}
	for _, forbidden := range []string{`"version"`, "futureTop", "futureData"} {
		if strings.Contains(jsonOutput, forbidden) {
			t.Errorf("stable JSON contains %q:\n%s", forbidden, jsonOutput)
		}
	}

	jsonl, _, err := runCLI(srv.URL, "--jsonl", "relation", "list", "SOURCE01")
	if err != nil {
		t.Fatalf("JSONL relation list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 3 {
		t.Fatalf("JSONL lines = %d:\n%s", len(lines), jsonl)
	}
	for i, line := range lines {
		var record struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if record.Schema != 3 || record.Kind != "relation" {
			t.Fatalf("line %d = %#v", i, record)
		}
	}

	raw, _, err := runCLI(srv.URL, "--raw", "relation", "list", "SOURCE01")
	if err != nil {
		t.Fatalf("raw relation list: %v", err)
	}
	for _, want := range []string{`"version": ""`, "futureTop", "futureData", `"relations"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("raw output missing %q:\n%s", want, raw)
		}
	}
}

func TestRelationListEmptyMalformedAndInputErrors(t *testing.T) {
	srv := relationServer()
	defer srv.Close()

	human, _, err := runCLI(srv.URL, "relation", "list", "EMPTY001")
	if err != nil || human != "No relations for EMPTY001.\n" {
		t.Fatalf("empty human = %q, %v", human, err)
	}
	jsonOutput, _, err := runCLI(srv.URL, "--json", "relation", "list", "EMPTY001")
	if err != nil {
		t.Fatalf("empty JSON: %v", err)
	}
	if !strings.Contains(jsonOutput, `"data": []`) || !strings.Contains(jsonOutput, `"shown": 0`) {
		t.Fatalf("empty JSON = %s", jsonOutput)
	}
	jsonl, _, err := runCLI(srv.URL, "--jsonl", "relation", "list", "EMPTY001")
	if err != nil || jsonl != "" {
		t.Fatalf("empty JSONL = %q, %v", jsonl, err)
	}
	if raw, _, err := runCLI(srv.URL, "--raw", "relation", "list", "BROKEN01"); err != nil || !strings.Contains(raw, `"dc:relation": 42`) {
		t.Fatalf("raw malformed relations = %q, %v", raw, err)
	}

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing argument", args: []string{"relation", "list"}, want: "missing item key"},
		{name: "not found", args: []string{"relation", "list", "MISSING1"}, want: `no item with key "MISSING1"`},
		{name: "malformed stable", args: []string{"--json", "relation", "list", "BROKEN01"}, want: "target must be a string or array"},
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

func TestRelationListWebAndGroupLibraries(t *testing.T) {
	t.Run("web", func(t *testing.T) {
		t.Setenv("ZOTGO_API_KEY", "test-key")
		mux := http.NewServeMux()
		mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"userID":123,"username":"ada","access":{"user":{"library":true}}}`))
		})
		mux.HandleFunc("GET /users/123/items/WEBREL01", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Zotero-API-Key") != "test-key" {
				t.Error("missing API key")
			}
			_, _ = w.Write([]byte(`{"key":"WEBREL01","data":{"itemType":"book","relations":{"dc:relation":"http://zotero.org/users/123/items/TARGET01"}}}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "--web", "--json", "relation", "list", "WEBREL01")
		if err != nil {
			t.Fatalf("web relation list: %v", err)
		}
		if !strings.Contains(out, `"id": 123`) || !strings.Contains(out, `"targetKey": "TARGET01"`) {
			t.Fatalf("web output = %s", out)
		}
	})

	t.Run("group", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"id":42,"data":{"id":42,"name":"Research Group"}}]`))
		})
		mux.HandleFunc("GET /api/groups/42/items/GROUPREL", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"key":"GROUPREL","data":{"itemType":"book","relations":{"dc:relation":"http://zotero.org/groups/42/items/TARGET01"}}}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "-L", "Research Group", "--json", "relation", "list", "GROUPREL")
		if err != nil {
			t.Fatalf("group relation list: %v", err)
		}
		if !strings.Contains(out, `"type": "group"`) || !strings.Contains(out, `"id": 42`) {
			t.Fatalf("group output = %s", out)
		}
	})
}

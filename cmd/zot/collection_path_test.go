package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func collectionPathServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/collections", func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", `<`+baseURL+`/api/users/0/collections?start=2>; rel="next"`)
			w.Header().Set("Backoff", "0")
			_, _ = w.Write([]byte(`[
				{"key":"ROOT0001","version":"","meta":{"numItems":3},"data":{"key":"ROOT0001","name":"Research","parentCollection":false}},
				{"key":"MID00001","version":"","meta":{"numItems":2},"data":{"key":"MID00001","name":"Projects","parentCollection":"ROOT0001"}}
			]`))
		case "2":
			_, _ = w.Write([]byte(`[
				{"key":"LEAF0001","version":"","meta":{"numItems":1},"data":{"key":"LEAF0001","name":"Methods","parentCollection":"MID00001"}}
			]`))
		default:
			t.Errorf("unexpected start %q", r.URL.Query().Get("start"))
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	return srv, &requests
}

func TestCollectionPathHumanHelpAndPagination(t *testing.T) {
	srv, requests := collectionPathServer(t)
	defer srv.Close()

	help, _, err := runCLI(srv.URL, "collection", "path", "--help")
	if err != nil {
		t.Fatalf("collection path help: %v", err)
	}
	for _, want := range []string{"<collection-key>...", "root to leaf", "request order", "path array", "--raw is unavailable"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}

	out, _, err := runCLI(srv.URL, "collection", "path", "LEAF0001", "ROOT0001")
	if err != nil {
		t.Fatalf("collection path: %v", err)
	}
	for _, want := range []string{"KEY", "PATH", "LEAF0001", "Research / Projects / Methods", "ROOT0001", "2 collection paths"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "LEAF0001") > strings.LastIndex(out, "ROOT0001") {
		t.Fatalf("human output changed request order:\n%s", out)
	}
	if *requests != 2 {
		t.Fatalf("collection requests = %d, want two paginated requests", *requests)
	}
}

func TestCollectionPathJSONAndJSONL(t *testing.T) {
	srv, _ := collectionPathServer(t)
	defer srv.Close()

	out, _, err := runCLI(srv.URL, "--json", "collection", "path", "LEAF0001", "ROOT0001")
	if err != nil {
		t.Fatalf("collection path JSON: %v", err)
	}
	var document struct {
		Schema int    `json:"schema"`
		Kind   string `json:"kind"`
		Data   []struct {
			Key       string `json:"key"`
			Name      string `json:"name"`
			ParentKey string `json:"parentKey"`
			NumItems  int    `json:"numItems"`
			Path      []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"path"`
		} `json:"data"`
		Meta struct {
			Shown int `json:"shown"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if document.Schema != 2 || document.Kind != "collections" || document.Meta.Shown != 2 || document.Meta.Total != 2 || len(document.Data) != 2 {
		t.Fatalf("document = %#v", document)
	}
	leaf := document.Data[0]
	if leaf.Key != "LEAF0001" || leaf.Name != "Methods" || leaf.ParentKey != "MID00001" || leaf.NumItems != 1 || len(leaf.Path) != 3 {
		t.Fatalf("leaf = %#v", leaf)
	}
	if leaf.Path[0].Key != "ROOT0001" || leaf.Path[2].Key != "LEAF0001" {
		t.Fatalf("path = %#v", leaf.Path)
	}
	if document.Data[1].Key != "ROOT0001" || document.Data[1].ParentKey != "" || len(document.Data[1].Path) != 1 {
		t.Fatalf("root = %#v", document.Data[1])
	}
	if strings.Contains(out, `"version"`) || strings.Contains(out, `"displayPath"`) {
		t.Fatalf("stable JSON leaked an unstable field:\n%s", out)
	}

	jsonl, _, err := runCLI(srv.URL, "--jsonl", "collection", "path", "ROOT0001", "LEAF0001", "ROOT0001")
	if err != nil {
		t.Fatalf("collection path JSONL: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 3 {
		t.Fatalf("JSONL lines = %d:\n%s", len(lines), jsonl)
	}
	wantKeys := []string{"ROOT0001", "LEAF0001", "ROOT0001"}
	for i, line := range lines {
		var record struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
			Data   struct {
				Key string `json:"key"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if record.Schema != 2 || record.Kind != "collection" || record.Data.Key != wantKeys[i] {
			t.Errorf("line %d = %#v", i, record)
		}
	}
}

func TestCollectionPathRejectsRawAndBadInputBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "raw", args: []string{"--raw", "collection", "path", "ROOT0001"}, want: "derived from multiple collection records"},
		{name: "missing", args: []string{"collection", "path"}, want: "missing collection key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, _, err := runCLI("http://127.0.0.1:1", test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
			if stdout != "" {
				t.Fatalf("stdout after error = %q", stdout)
			}
		})
	}
}

func TestCollectionPathMissingCollectionHasNoOutput(t *testing.T) {
	srv, _ := collectionPathServer(t)
	defer srv.Close()
	stdout, _, err := runCLI(srv.URL, "collection", "path", "LOST0001")
	if err == nil || !strings.Contains(err.Error(), `no collection with key "LOST0001"`) {
		t.Fatalf("error = %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout after error = %q", stdout)
	}
}

func TestCollectionPathWebAndGroupLibraries(t *testing.T) {
	t.Run("web", func(t *testing.T) {
		t.Setenv("ZOTGO_API_KEY", "test-key")
		mux := http.NewServeMux()
		mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"userID":77,"username":"ada","access":{"user":{"library":true}}}`))
		})
		mux.HandleFunc("GET /users/77/collections", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Zotero-API-Key") != "test-key" {
				t.Error("missing API key")
			}
			_, _ = w.Write([]byte(`[
				{"key":"WEBROOT1","data":{"key":"WEBROOT1","name":"Web Root","parentCollection":false}},
				{"key":"WEBLEAF1","data":{"key":"WEBLEAF1","name":"Web Leaf","parentCollection":"WEBROOT1"}}
			]`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "--web", "--json", "collection", "path", "WEBLEAF1")
		if err != nil {
			t.Fatalf("web collection path: %v", err)
		}
		if !strings.Contains(out, `"id": 77`) || !strings.Contains(out, `"name": "Web Root"`) {
			t.Fatalf("web output = %s", out)
		}
	})

	t.Run("group", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"id":42,"data":{"id":42,"name":"Research Group"}}]`))
		})
		mux.HandleFunc("GET /api/groups/42/collections", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[
				{"key":"GROUPRT1","data":{"key":"GROUPRT1","name":"Group Root","parentCollection":false}},
				{"key":"GROUPLF1","data":{"key":"GROUPLF1","name":"Group Leaf","parentCollection":"GROUPRT1"}}
			]`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "-L", "Research Group", "--json", "collection", "path", "GROUPLF1")
		if err != nil {
			t.Fatalf("group collection path: %v", err)
		}
		if !strings.Contains(out, `"type": "group"`) || !strings.Contains(out, `"id": 42`) {
			t.Fatalf("group output = %s", out)
		}
	})
}

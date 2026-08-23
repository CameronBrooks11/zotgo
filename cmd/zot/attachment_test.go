package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const importedAttachmentEnvelope = `{
	"key":"ATTACH01",
	"version":"",
	"futureTop":{"kept":true},
	"links":{"enclosure":{"href":"http://local/file","type":"application/pdf","title":"paper.pdf","length":1234,"futureLink":true}},
	"data":{
		"key":"ATTACH01","version":"","itemType":"attachment","parentItem":"PARENT01",
		"title":"Full Text PDF","linkMode":"imported_file","contentType":"application/pdf",
		"filename":"paper.pdf","url":"https://example.com/paper.pdf","accessDate":"2026-01-01T00:00:00Z",
		"dateAdded":"2026-01-02T00:00:00Z","dateModified":"2026-01-03T00:00:00Z",
		"tags":[{"tag":"review","type":0}],"md5":"0123456789abcdef","mtime":1700000000000,"futureData":7
	}
}`

func attachmentServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var itemReads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		itemReads.Add(1)
		switch r.PathValue("key") {
		case "ATTACH01":
			_, _ = w.Write([]byte(importedAttachmentEnvelope))
		case "LINKURL1":
			_, _ = w.Write([]byte(`{"key":"LINKURL1","data":{"itemType":"attachment","parentItem":false,"title":"Website","linkMode":"linked_url","contentType":"text/html","url":"https://example.com","tags":[],"md5":null,"mtime":null}}`))
		case "WRONG001":
			_, _ = w.Write([]byte(`{"key":"WRONG001","data":{"itemType":"book","title":"Not an attachment"}}`))
		case "BADKEY01":
			_, _ = w.Write([]byte(`{"key":"OTHER001","data":{"itemType":"attachment"}}`))
		case "RAWSHAPE":
			_, _ = w.Write([]byte(`{"key":"RAWSHAPE","futureTop":true,"links":[],"data":{"itemType":"attachment","parentItem":{},"md5":7,"mtime":"future-shape"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("GET /api/users/0/items/{key}/file", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("attachment show requested file bytes: %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	return httptest.NewServer(mux), &itemReads
}

func TestAttachmentShowHelpAndHuman(t *testing.T) {
	srv, reads := attachmentServer(t)
	defer srv.Close()

	rootHelp, _, err := runCLI(srv.URL, "--help")
	if err != nil {
		t.Fatalf("root help: %v", err)
	}
	if !strings.Contains(rootHelp, "attachment") {
		t.Fatalf("root help missing attachment:\n%s", rootHelp)
	}
	showHelp, _, err := runCLI(srv.URL, "attachment", "show", "--help")
	if err != nil {
		t.Fatalf("attachment show help: %v", err)
	}
	for _, want := range []string{"<attachment-key>", "show attachment metadata", "without opening", "complete Zotero envelope"} {
		if !strings.Contains(showHelp, want) {
			t.Errorf("show help missing %q:\n%s", want, showHelp)
		}
	}

	out, _, err := runCLI(srv.URL, "attachment", "show", "ATTACH01")
	if err != nil {
		t.Fatalf("attachment show: %v", err)
	}
	for _, want := range []string{
		"ATTACH01", "PARENT01", "Full Text PDF", "imported_file", "application/pdf", "paper.pdf",
		"0123456789abcdef", "1700000000000", "1234",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("item reads = %d, want one", reads.Load())
	}
}

func TestAttachmentShowMachineOutput(t *testing.T) {
	srv, _ := attachmentServer(t)
	defer srv.Close()

	for _, mode := range []string{"--json", "--jsonl"} {
		out, _, err := runCLI(srv.URL, mode, "attachment", "show", "ATTACH01")
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if mode == "--jsonl" && strings.Count(strings.TrimSpace(out), "\n") != 0 {
			t.Fatalf("JSONL has multiple lines:\n%s", out)
		}
		var doc struct {
			Schema int            `json:"schema"`
			Kind   string         `json:"kind"`
			Data   map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("decode %s: %v\n%s", mode, err, out)
		}
		if doc.Schema != 3 || doc.Kind != "attachment" || doc.Data["key"] != "ATTACH01" || doc.Data["parentKey"] != "PARENT01" {
			t.Fatalf("%s document = %#v", mode, doc)
		}
		if len(doc.Data) != 15 {
			t.Fatalf("attachment fields = %v", doc.Data)
		}
		for _, forbidden := range []string{`"version"`, "futureTop", "futureData", "futureLink"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("stable output leaked %q:\n%s", forbidden, out)
			}
		}
	}
}

func TestAttachmentShowWebProfileRouting(t *testing.T) {
	var itemPath, apiKey string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userID":12345,"username":"ada","access":{"user":{"library":true,"write":false}}}`))
	})
	mux.HandleFunc("GET /users/12345/items/WEBATT01", func(w http.ResponseWriter, r *http.Request) {
		itemPath = r.URL.Path
		apiKey = r.Header.Get("Zotero-API-Key")
		_, _ = w.Write([]byte(`{"key":"WEBATT01","data":{"itemType":"attachment","parentItem":"PARENT01","title":"Web PDF","linkMode":"imported_file","contentType":"application/pdf","filename":"web.pdf","tags":[],"mtime":"1331171741767"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("ZOTGO_API_KEY", "test-key")

	out, _, err := runCLI(srv.URL, "--json", "--web", "attachment", "show", "WEBATT01")
	if err != nil {
		t.Fatalf("attachment show --web: %v", err)
	}
	if itemPath != "/users/12345/items/WEBATT01" || apiKey != "test-key" {
		t.Fatalf("request = %q with key %q", itemPath, apiKey)
	}
	if !strings.Contains(out, `"key": "WEBATT01"`) || !strings.Contains(out, `"kind": "attachment"`) || !strings.Contains(out, `"mtime": 1331171741767`) {
		t.Fatalf("web output:\n%s", out)
	}
}

func TestAttachmentShowRawPreservesCompleteEnvelope(t *testing.T) {
	srv, _ := attachmentServer(t)
	defer srv.Close()

	for _, tt := range []struct {
		key     string
		fixture string
	}{
		{key: "ATTACH01", fixture: importedAttachmentEnvelope},
		{key: "RAWSHAPE", fixture: `{"key":"RAWSHAPE","futureTop":true,"links":[],"data":{"itemType":"attachment","parentItem":{},"md5":7,"mtime":"future-shape"}}`},
	} {
		out, _, err := runCLI(srv.URL, "--raw", "attachment", "show", tt.key)
		if err != nil {
			t.Fatalf("attachment show raw %s: %v", tt.key, err)
		}
		var got, want any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode raw: %v", err)
		}
		if err := json.Unmarshal([]byte(tt.fixture), &want); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("raw changed envelope:\n got: %#v\nwant: %#v", got, want)
		}
	}
}

func TestAttachmentShowValidatesBeforeAllOutput(t *testing.T) {
	srv, _ := attachmentServer(t)
	defer srv.Close()

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing argument", args: []string{"attachment", "show"}, want: "missing attachment key"},
		{name: "not found", args: []string{"attachment", "show", "MISSING1"}, want: `no attachment with key "MISSING1"`},
		{name: "wrong type stable", args: []string{"--json", "attachment", "show", "WRONG001"}, want: `type "book", not attachment`},
		{name: "wrong type raw", args: []string{"--raw", "attachment", "show", "WRONG001"}, want: `type "book", not attachment`},
		{name: "key mismatch raw", args: []string{"--raw", "attachment", "show", "BADKEY01"}, want: `response has key "OTHER001"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := runCLI(srv.URL, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if stdout != "" {
				t.Fatalf("stdout after validation error = %q", stdout)
			}
		})
	}
}

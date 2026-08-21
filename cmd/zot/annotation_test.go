package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const annotationFirst = `{"key":"ANN00002","futureTop":{"kept":2},"data":{"key":"ANN00002","itemType":"annotation","parentItem":"ATTACH01","annotationType":"note","annotationPageLabel":"13","annotationColor":"#ff6666","annotationSortIndex":"00002","annotationText":"","annotationComment":"private comment","annotationPosition":{"pageIndex":12},"futureData":true}}`
const annotationSecond = `{"key":"ANN00001","futureTop":{"kept":1},"data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight","annotationPageLabel":"12","annotationColor":"#ffd400","annotationSortIndex":"00001","annotationText":"private quotation","annotationComment":"","annotationPosition":"{\"pageIndex\":11}"}}`

func newAnnotationServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"ATTACH01","version":"","data":{"key":"ATTACH01","itemType":"attachment","linkMode":"imported_file"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/ATTACH01/children", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("itemType") != "annotation" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("child query = %s", r.URL.RawQuery)
		}
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items/ATTACH01/children?itemType=annotation&limit=100&start=1>; rel="next"`, baseURL))
			_, _ = fmt.Fprintf(w, "[%s]", annotationFirst)
		case "1":
			_, _ = fmt.Fprintf(w, "[%s]", annotationSecond)
		default:
			t.Errorf("unexpected start %q", r.URL.Query().Get("start"))
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	return srv, &requests
}

func TestAnnotationListHumanHelpAndPagination(t *testing.T) {
	srv, requests := newAnnotationServer(t)
	defer srv.Close()
	help, _, err := runCLI(srv.URL, "annotation", "list", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"<attachment-key>", "compact metadata", "without annotation text", "image bytes", "--raw"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
	out, _, err := runCLI(srv.URL, "annotation", "list", "ATTACH01")
	if err != nil {
		t.Fatalf("annotation list: %v", err)
	}
	for _, want := range []string{"KEY", "PAGE", "SORT INDEX", "TYPE", "TEXT", "COMMENT", "COLOR", "ANN00001", "highlight", "ANN00002", "note", "2 annotations"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "ANN00001") > strings.Index(out, "ANN00002") {
		t.Fatalf("stable output not sorted in document order:\n%s", out)
	}
	if strings.Contains(out, "private quotation") || strings.Contains(out, "private comment") {
		t.Fatalf("human output leaked bodies:\n%s", out)
	}
	if *requests != 2 {
		t.Fatalf("child requests = %d, want 2", *requests)
	}
}

func TestAnnotationListJSONAndJSONL(t *testing.T) {
	srv, _ := newAnnotationServer(t)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "annotation", "list", "ATTACH01")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var document struct {
		Schema int    `json:"schema"`
		Kind   string `json:"kind"`
		Data   []struct {
			Key           string `json:"key"`
			AttachmentKey string `json:"attachmentKey"`
			Type          string `json:"type"`
			PageLabel     string `json:"pageLabel"`
			Color         string `json:"color"`
			SortIndex     string `json:"sortIndex"`
			HasText       bool   `json:"hasText"`
			HasComment    bool   `json:"hasComment"`
		} `json:"data"`
		Meta struct {
			Shown int `json:"shown"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if document.Schema != 2 || document.Kind != "annotations" || len(document.Data) != 2 || document.Meta.Shown != 2 || document.Meta.Total != 2 {
		t.Fatalf("document = %#v", document)
	}
	if first := document.Data[0]; first.Key != "ANN00001" || first.AttachmentKey != "ATTACH01" || first.Type != "highlight" || first.PageLabel != "12" || first.Color != "#ffd400" || first.SortIndex != "00001" || !first.HasText || first.HasComment {
		t.Fatalf("first annotation = %#v", first)
	}
	for _, forbidden := range []string{"private quotation", "private comment", "annotationText", "annotationComment", "annotationPosition", "futureData", "version"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("stable JSON leaked %q:\n%s", forbidden, out)
		}
	}

	jsonl, _, err := runCLI(srv.URL, "--jsonl", "annotation", "list", "ATTACH01")
	if err != nil {
		t.Fatalf("JSONL: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 2 {
		t.Fatalf("JSONL lines = %d:\n%s", len(lines), jsonl)
	}
	for i, wantKey := range []string{"ANN00001", "ANN00002"} {
		var record struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
			Data   struct {
				Key string `json:"key"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &record); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if record.Schema != 2 || record.Kind != "annotation" || record.Data.Key != wantKey {
			t.Errorf("line %d = %#v", i, record)
		}
	}
}

func TestAnnotationListRawPreservesServerOrderAndUnknownFields(t *testing.T) {
	srv, _ := newAnnotationServer(t)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--raw", "annotation", "list", "ATTACH01")
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("decode raw: %v\n%s", err, out)
	}
	if len(items) != 2 || items[0]["key"] != "ANN00002" || items[1]["key"] != "ANN00001" {
		t.Fatalf("raw order/items = %#v", items)
	}
	if _, ok := items[0]["futureTop"]; !ok {
		t.Fatalf("raw omitted future top-level field: %s", out)
	}
	data := items[0]["data"].(map[string]any)
	if data["annotationComment"] != "private comment" || data["futureData"] != true {
		t.Fatalf("raw data = %#v", data)
	}
	if strings.Contains(out, `"schema"`) || strings.Contains(out, `"kind"`) {
		t.Fatalf("raw output gained stable wrapper: %s", out)
	}
}

func TestAnnotationListEmptyOutputs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/ATTACH01/children", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, test := range []struct {
		name string
		flag string
		want string
	}{
		{name: "human", want: "No annotations for attachment ATTACH01."},
		{name: "json", flag: "--json", want: `"data": []`},
		{name: "raw", flag: "--raw", want: "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"annotation", "list", "ATTACH01"}
			if test.flag != "" {
				args = append([]string{test.flag}, args...)
			}
			out, _, err := runCLI(srv.URL, args...)
			if err != nil || !strings.Contains(out, test.want) {
				t.Fatalf("output=%s error=%v, want %q", out, err, test.want)
			}
		})
	}
}

func TestAnnotationListRejectsArgumentsBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: []string{"annotation", "list"}, want: "missing attachment key"},
		{name: "extra", args: []string{"annotation", "list", "ATTACH01", "EXTRA001"}, want: "exactly one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, _, err := runCLI("http://127.0.0.1:1", test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) || out != "" {
				t.Fatalf("output=%q error=%v", out, err)
			}
		})
	}
}

func TestAnnotationListParentAndBufferedErrorsHaveNoOutput(t *testing.T) {
	for _, test := range []struct {
		name    string
		parent  string
		first   string
		second  string
		status  int
		wantErr string
	}{
		{name: "missing parent", status: http.StatusNotFound, wantErr: "no attachment with key"},
		{name: "wrong parent type", parent: `{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"book"}}`, wantErr: `not attachment`},
		{name: "later page", parent: `{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`, first: `[` + annotationFirst + `]`, status: http.StatusInternalServerError, wantErr: "HTTP 500"},
		{name: "malformed stable annotation", parent: `{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`, first: `[{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight","annotationText":null}}]`, wantErr: "must be a string, not null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var baseURL string
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/users/0/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
				if test.status == http.StatusNotFound && test.parent == "" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = w.Write([]byte(test.parent))
			})
			mux.HandleFunc("GET /api/users/0/items/ATTACH01/children", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("start") == "1" {
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.second))
					return
				}
				if test.name == "later page" {
					w.Header().Set("Link", `<`+baseURL+`/api/users/0/items/ATTACH01/children?start=1>; rel="next"`)
				}
				_, _ = w.Write([]byte(test.first))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			baseURL = srv.URL
			out, _, err := runCLI(srv.URL, "--json", "annotation", "list", "ATTACH01")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) || out != "" {
				t.Fatalf("output=%q error=%v, want %q", out, err, test.wantErr)
			}
		})
	}
}

func TestAnnotationListRawEscapeAcceptsFutureStableShapes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/ATTACH01/children", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"ANN00001","future":true,"data":{"key":"ANN00001","itemType":"annotation","annotationText":{"future":"shape"}}}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--raw", "annotation", "list", "ATTACH01")
	if err != nil || !strings.Contains(out, `"future"`) || !strings.Contains(out, `"annotationText"`) {
		t.Fatalf("output=%s error=%v", out, err)
	}
}

func TestAnnotationListWebAndGroupRouting(t *testing.T) {
	t.Run("web", func(t *testing.T) {
		t.Setenv("ZOTGO_API_KEY", "test-key")
		mux := http.NewServeMux()
		mux.HandleFunc("GET /keys/current", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"userID":77,"username":"ada","access":{"user":{"library":true}}}`))
		})
		mux.HandleFunc("GET /users/77/items/ATTACH01", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Zotero-API-Key") != "test-key" {
				t.Error("missing Web API key")
			}
			_, _ = w.Write([]byte(`{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`))
		})
		mux.HandleFunc("GET /users/77/items/ATTACH01/children", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Zotero-API-Key") != "test-key" || r.URL.Query().Get("itemType") != "annotation" {
				t.Errorf("web request headers/query = %#v / %s", r.Header, r.URL.RawQuery)
			}
			_, _ = fmt.Fprintf(w, "[%s]", annotationSecond)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "--web", "--json", "annotation", "list", "ATTACH01")
		if err != nil || !strings.Contains(out, `"id": 77`) || !strings.Contains(out, `"key": "ANN00001"`) {
			t.Fatalf("output=%s error=%v", out, err)
		}
	})

	t.Run("group", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"id":42,"data":{"id":42,"name":"Research Group"}}]`))
		})
		mux.HandleFunc("GET /api/groups/42/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`))
		})
		mux.HandleFunc("GET /api/groups/42/items/ATTACH01/children", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, "[%s]", annotationSecond)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		out, _, err := runCLI(srv.URL, "-L", "Research Group", "--json", "annotation", "list", "ATTACH01")
		if err != nil || !strings.Contains(out, `"type": "group"`) || !strings.Contains(out, `"id": 42`) {
			t.Fatalf("output=%s error=%v", out, err)
		}
	})
}

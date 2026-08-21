package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func childResponse(r *http.Request, status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     header,
		Request:    r,
	}
}

func TestRawChildItems_LocalUserRouteQueryAndEscaping(t *testing.T) {
	var seen *http.Request
	client := New("http://localhost:23119", WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			header := http.Header{}
			header.Set("Total-Results", "12")
			return childResponse(r, http.StatusOK, `[]`, header), nil
		}),
	}))

	children, page, err := client.RawChildItems(context.Background(), UserLibrary(), "PARENT/KEY", ChildItemsOptions{
		ItemType: "attachment || note",
		Limit:    25,
		Start:    50,
	})
	if err != nil {
		t.Fatalf("RawChildItems: %v", err)
	}
	if seen == nil {
		t.Fatal("no request captured")
	}
	if got, want := seen.URL.EscapedPath(), "/api/users/0/items/PARENT%2FKEY/children"; got != want {
		t.Fatalf("escaped path = %q, want %q", got, want)
	}
	wantQuery := map[string][]string{
		"itemType": {"attachment || note"},
		"limit":    {"25"},
		"start":    {"50"},
	}
	if got := seen.URL.Query(); !reflect.DeepEqual(map[string][]string(got), wantQuery) {
		t.Fatalf("query = %#v, want %#v", got, wantQuery)
	}
	if children == nil || len(children) != 0 {
		t.Fatalf("children = %#v, want nonnil empty slice", children)
	}
	if page.TotalResults != 12 {
		t.Fatalf("TotalResults = %d, want 12", page.TotalResults)
	}
}

func TestRawChildItems_GroupRoute(t *testing.T) {
	var seen *http.Request
	client := New("http://localhost:23119", WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			return childResponse(r, http.StatusOK, `[]`, nil), nil
		}),
	}))

	if _, _, err := client.RawChildItems(context.Background(), GroupLibrary(42, "Group"), "PARENT", ChildItemsOptions{}); err != nil {
		t.Fatalf("RawChildItems: %v", err)
	}
	if got, want := seen.URL.Path, "/api/groups/42/items/PARENT/children"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestRawChildItems_WebRouteAndAuthentication(t *testing.T) {
	var seen *http.Request
	client := NewWeb("https://api.zotero.org", "secret", WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			return childResponse(r, http.StatusOK, `[]`, nil), nil
		}),
	}))
	library := LibraryRef{Kind: LibraryKindUser, ID: 12345}

	if _, _, err := client.RawChildItems(context.Background(), library, "PARENT", ChildItemsOptions{}); err != nil {
		t.Fatalf("RawChildItems: %v", err)
	}
	if got, want := seen.URL.Path, "/users/12345/items/PARENT/children"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got := seen.Header.Get("Zotero-API-Key"); got != "secret" {
		t.Fatalf("Zotero-API-Key = %q, want secret", got)
	}
}

func TestAllRawChildItems_PaginatesFromStartAndPreservesOptions(t *testing.T) {
	var starts []string
	var filters []string
	var limits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("start"))
		filters = append(filters, r.URL.Query().Get("itemType"))
		limits = append(limits, r.URL.Query().Get("limit"))
		switch r.URL.Query().Get("start") {
		case "5":
			w.Header().Set("Link", `<http://example.test/items?start=8>; rel="next"`)
			_, _ = io.WriteString(w, `[{"key":"CHILD1","data":{"itemType":"attachment"}}]`)
		case "8":
			_, _ = io.WriteString(w, `[{"key":"CHILD2","data":{"itemType":"note"}}]`)
		default:
			t.Errorf("unexpected start %q", r.URL.Query().Get("start"))
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	defer server.Close()

	children, err := New(server.URL).AllRawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{
		ItemType: "attachment || note",
		Limit:    3,
		Start:    5,
	})
	if err != nil {
		t.Fatalf("AllRawChildItems: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("len(children) = %d, want 2", len(children))
	}
	first, _ := DecodeItemIdentity(children[0])
	second, _ := DecodeItemIdentity(children[1])
	if first.Key != "CHILD1" || second.Key != "CHILD2" {
		t.Fatalf("child order = %q, %q", first.Key, second.Key)
	}
	if !reflect.DeepEqual(starts, []string{"5", "8"}) {
		t.Fatalf("starts = %#v, want [5 8]", starts)
	}
	if !reflect.DeepEqual(filters, []string{"attachment || note", "attachment || note"}) {
		t.Fatalf("filters = %#v", filters)
	}
	if !reflect.DeepEqual(limits, []string{"3", "3"}) {
		t.Fatalf("limits = %#v", limits)
	}
}

func TestAllRawChildItems_HonorsBackoffBeforeNextPage(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("start") == "" {
			w.Header().Set("Backoff", "2")
			w.Header().Set("Link", `<http://example.test/items?start=1>; rel="next"`)
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()

	client := New(server.URL)
	var waits []time.Duration
	client.sleep = func(_ context.Context, d time.Duration) error {
		if requests != 1 {
			t.Errorf("sleep called after %d requests, want before second request", requests)
		}
		waits = append(waits, d)
		return nil
	}
	if _, err := client.AllRawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{}); err != nil {
		t.Fatalf("AllRawChildItems: %v", err)
	}
	if !reflect.DeepEqual(waits, []time.Duration{2 * time.Second}) {
		t.Fatalf("waits = %#v, want [2s]", waits)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestAllRawChildItems_LaterPageFailuresReturnNoPartialSlice(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "http", statusCode: http.StatusInternalServerError, body: `failed`},
		{name: "json", statusCode: http.StatusOK, body: `[`},
		{name: "identity", statusCode: http.StatusOK, body: `[{"key":"CHILD2","data":{}}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("start") == "" {
					w.Header().Set("Link", `<http://example.test/items?start=1>; rel="next"`)
					_, _ = io.WriteString(w, `[{"key":"CHILD1","data":{"itemType":"note"}}]`)
					return
				}
				w.WriteHeader(test.statusCode)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			children, err := New(server.URL).AllRawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if children != nil {
				t.Fatalf("children = %#v, want nil on later-page error", children)
			}
		})
	}
}

func TestRawChildItems_StrictTopLevelArray(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "whitespace", body: " \n\t"},
		{name: "null", body: `null`},
		{name: "object", body: `{}`},
		{name: "malformed", body: `[{"key":"CHILD"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			if _, _, err := New(server.URL).RawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRawChildItems_EmptyArrayIsNonnil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()

	client := New(server.URL)
	page, _, err := client.RawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{})
	if err != nil {
		t.Fatalf("RawChildItems: %v", err)
	}
	if page == nil || len(page) != 0 {
		t.Fatalf("RawChildItems = %#v, want nonnil empty", page)
	}
	all, err := client.AllRawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{})
	if err != nil {
		t.Fatalf("AllRawChildItems: %v", err)
	}
	if all == nil || len(all) != 0 {
		t.Fatalf("AllRawChildItems = %#v, want nonnil empty", all)
	}
}

func TestRawChildItems_RejectsInvalidChildIdentityAndParentKey(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing top key", body: `[{"data":{"itemType":"note"}}]`},
		{name: "missing item type", body: `[{"key":"CHILD","data":{}}]`},
		{name: "repeated parent key", body: `[{"key":"PARENT","data":{"itemType":"note"}}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			if _, _, err := New(server.URL).RawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRawChildItems_PreservesUnknownFieldsSemantically(t *testing.T) {
	body := `[
		{"key":"CHILD1","futureEnvelope":{"enabled":true},"data":{"itemType":"attachment","futureData":[1,{"x":"y"}]}},
		{"key":"CHILD2","futureNumber":17,"data":{"itemType":"note","text":"second"}}
	]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	children, _, err := New(server.URL).RawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{})
	if err != nil {
		t.Fatalf("RawChildItems: %v", err)
	}
	var got []any
	if err := json.Unmarshal([]byte("["+string(children[0])+","+string(children[1])+"]"), &got); err != nil {
		t.Fatalf("decode returned children: %v", err)
	}
	var want []any
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("returned JSON lost or changed fields:\n got %#v\nwant %#v", got, want)
	}
}

func TestAllRawChildItems_RejectsBadOrNonadvancingCursor(t *testing.T) {
	tests := []struct {
		name string
		link string
	}{
		{name: "missing start", link: `<http://example.test/items>; rel="next"`},
		{name: "nonadvancing", link: `<http://example.test/items?start=4>; rel="next"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Link", test.link)
				_, _ = io.WriteString(w, `[]`)
			}))
			defer server.Close()

			children, err := New(server.URL).AllRawChildItems(context.Background(), UserLibrary(), "PARENT", ChildItemsOptions{Start: 4})
			if !errors.Is(err, ErrBadPagination) {
				t.Fatalf("err = %v, want ErrBadPagination", err)
			}
			if children != nil {
				t.Fatalf("children = %#v, want nil", children)
			}
		})
	}
}

func TestRawChildItems_EmptyParentRejectedBeforeNetwork(t *testing.T) {
	requests := 0
	client := New("http://localhost:23119", WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			return childResponse(r, http.StatusOK, `[]`, nil), nil
		}),
	}))

	if _, _, err := client.RawChildItems(context.Background(), UserLibrary(), "", ChildItemsOptions{}); err == nil {
		t.Fatal("expected an error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
	if children, err := client.AllRawChildItems(context.Background(), UserLibrary(), "", ChildItemsOptions{}); err == nil || children != nil {
		t.Fatalf("AllRawChildItems = (%#v, %v), want nil and error", children, err)
	}
	if requests != 0 {
		t.Fatalf("requests after AllRawChildItems = %d, want 0", requests)
	}
}

// AllRawAnnotations rejects a non-attachment parent, and honors the attachment
// parent's Backoff before requesting its annotation children.
func TestAllRawAnnotationsValidatesParentAndHonorsBackoff(t *testing.T) {
	nonAttachment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/users/0/items/BOOK0001" {
			_, _ = w.Write([]byte(`{"key":"BOOK0001","data":{"key":"BOOK0001","itemType":"book"}}`))
			return
		}
		t.Errorf("unexpected request to %s (children must not be fetched)", r.URL.Path)
	}))
	defer nonAttachment.Close()
	if _, err := New(nonAttachment.URL).AllRawAnnotations(context.Background(), UserLibrary(), "BOOK0001"); err == nil {
		t.Fatal("expected a non-attachment parent to be rejected before fetching children")
	}

	slept := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/ATTACH01", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Backoff", "7")
		_, _ = w.Write([]byte(`{"key":"ATTACH01","data":{"key":"ATTACH01","itemType":"attachment"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/ATTACH01/children", func(w http.ResponseWriter, r *http.Request) {
		if !slept {
			t.Error("children requested before parent Backoff was honored")
		}
		if got := r.URL.Query().Get("itemType"); got != "annotation" {
			t.Errorf("child itemType filter = %q, want annotation", got)
		}
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := New(srv.URL)
	client.sleep = func(_ context.Context, wait time.Duration) error {
		slept = true
		if wait != 7*time.Second {
			t.Errorf("wait = %v", wait)
		}
		return nil
	}
	if _, err := client.AllRawAnnotations(context.Background(), UserLibrary(), "ATTACH01"); err != nil {
		t.Fatalf("AllRawAnnotations: %v", err)
	}
	if !slept {
		t.Fatal("parent Backoff was not honored before the child request")
	}
}

// RawItemWithChildren (the show path) honors the parent's Backoff before its
// first child request. Ported from the pre-unification suite; it is the sole
// guard on the parent->first-child backoff edge.
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

// AllRawChildItems preserves each child's exact bytes, including numeric forms
// Go's json would renormalize (1e3 must not become 1000) — a byte-fidelity guard
// that reflect.DeepEqual-based tests cannot provide.
func TestAllRawChildItemsPreservesRawBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"CHILD02","data":{"itemType":"note","number":1e3}}]`))
	}))
	defer srv.Close()
	raw, err := New(srv.URL).AllRawChildItems(context.Background(), UserLibrary(), "PARENT01", ChildItemsOptions{})
	if err != nil {
		t.Fatalf("AllRawChildItems: %v", err)
	}
	if len(raw) != 1 || !strings.Contains(string(raw[0]), `"number":1e3`) {
		t.Fatalf("raw bytes not preserved verbatim: %q", raw)
	}
}

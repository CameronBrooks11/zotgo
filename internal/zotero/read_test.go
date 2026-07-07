package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestItems_ReadsPageAndAccessors(t *testing.T) {
	var sawAPIVersion bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items/top", func(w http.ResponseWriter, r *http.Request) {
		sawAPIVersion = r.Header.Get("Zotero-API-Version") == "3"
		if got := r.URL.Query().Get("q"); got != "algae" {
			t.Errorf("q = %q, want algae", got)
		}
		w.Header().Set("Total-Results", "1")
		w.Header().Set("Last-Modified-Version", "3579")
		w.Header().Set("Zotero-Schema-Version", "42")
		w.Header().Set("Zotero-API-Version", "3")
		_, _ = w.Write([]byte(`[
			{
				"key":"HRAC4E44",
				"version":3579,
				"library":{"type":"user","id":8784047,"name":"My Library"},
				"links":{"attachment":{"href":"http://localhost/items/CWEW5DNC","attachmentType":"application/pdf","attachmentSize":585858}},
				"meta":{"creatorSummary":"Posten","parsedDate":"2009-06","numChildren":1},
				"data":{"key":"HRAC4E44","version":3579,"itemType":"journalArticle","title":"Algae paper","unknownFutureField":"kept"}
			}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, page, err := New(srv.URL).Items(context.Background(), UserLibrary(), ItemsOptions{
		Top:        true,
		Query:      "algae",
		Everything: true,
	})
	if err != nil {
		t.Fatalf("Items returned error: %v", err)
	}
	if !sawAPIVersion {
		t.Fatal("request did not include Zotero-API-Version: 3")
	}
	if page.TotalResults != 1 || page.LastModifiedVersion != "3579" || page.SchemaVersion != "42" {
		t.Fatalf("unexpected page headers: %+v", page)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if got := items[0].Title(); got != "Algae paper" {
		t.Errorf("Title = %q, want Algae paper", got)
	}
	if got := items[0].ItemType(); got != "journalArticle" {
		t.Errorf("ItemType = %q, want journalArticle", got)
	}
	if got := items[0].CreatorSummary(); got != "Posten" {
		t.Errorf("CreatorSummary = %q, want Posten", got)
	}
	if got := items[0].NumChildren(); got != 1 {
		t.Errorf("NumChildren = %d, want 1", got)
	}
	var raw map[string]any
	if err := json.Unmarshal(items[0].Data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["unknownFutureField"] != "kept" {
		t.Errorf("raw data did not preserve unknown field: %#v", raw)
	}
}

func TestAllItems_FollowsNextLink(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Total-Results", "2")
		w.Header().Set("Zotero-Schema-Version", "42")
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", "<"+srv.URL+"/api/users/0/items?limit=1&start=1>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"key":"AAAA1111","data":{"itemType":"book","title":"One"}}]`))
		case "1":
			_, _ = w.Write([]byte(`[{"key":"BBBB2222","data":{"itemType":"book","title":"Two"}}]`))
		default:
			t.Fatalf("unexpected start = %q", r.URL.Query().Get("start"))
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	items, err := New(srv.URL).AllItems(context.Background(), UserLibrary(), ItemsOptions{Limit: 1})
	if err != nil {
		t.Fatalf("AllItems returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Key != "AAAA1111" || items[1].Key != "BBBB2222" {
		t.Fatalf("unexpected keys: %+v", items)
	}
}

func TestLocalAPIDisabledError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Local API is not enabled"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, _, err := New(srv.URL).Items(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrLocalAPIDisabled) {
		t.Fatalf("err = %v, want ErrLocalAPIDisabled", err)
	}
}

func TestZoteroDownError(t *testing.T) {
	// A started-then-closed server yields a definitely-refused address.
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close()

	_, _, err := New(url).Items(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrZoteroDown) {
		t.Fatalf("err = %v, want ErrZoteroDown", err)
	}
}

func TestResolveLibrary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":101,"version":1,"meta":{"numItems":12},"data":{"id":101,"name":"Energy Market","description":""}},
			{"id":202,"version":1,"meta":{"numItems":7},"data":{"id":202,"name":"Power Flow","description":""}}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL)
	user, err := client.ResolveLibrary(context.Background(), "me")
	if err != nil {
		t.Fatalf("ResolveLibrary(me): %v", err)
	}
	if user.Prefix() != "/api/users/0" {
		t.Fatalf("user prefix = %q", user.Prefix())
	}

	byName, err := client.ResolveLibrary(context.Background(), "Energy Market")
	if err != nil {
		t.Fatalf("ResolveLibrary(name): %v", err)
	}
	if byName.Prefix() != "/api/groups/101" || byName.Name != "Energy Market" {
		t.Fatalf("byName = %+v", byName)
	}

	byID, err := client.ResolveLibrary(context.Background(), "groups/202")
	if err != nil {
		t.Fatalf("ResolveLibrary(id): %v", err)
	}
	if byID.Prefix() != "/api/groups/202" || byID.Name != "Power Flow" {
		t.Fatalf("byID = %+v", byID)
	}
}

func TestResolveLibraryAmbiguousName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":1,"data":{"id":1,"name":"Shared"}},
			{"id":2,"data":{"id":2,"name":"Shared"}}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := New(srv.URL).ResolveLibrary(context.Background(), "Shared")
	if !errors.Is(err, ErrAmbiguousLibrary) {
		t.Fatalf("err = %v, want ErrAmbiguousLibrary", err)
	}
}

func TestItemChildrenAndCollectionsRoutes(t *testing.T) {
	var sawChildren, sawCollections bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/groups/42/items/ABCD1234/children", func(w http.ResponseWriter, _ *http.Request) {
		sawChildren = true
		_, _ = w.Write([]byte(`[{"key":"CHILD123","data":{"itemType":"attachment","title":"PDF"}}]`))
	})
	mux.HandleFunc("/api/groups/42/collections/top", func(w http.ResponseWriter, _ *http.Request) {
		sawCollections = true
		_, _ = w.Write([]byte(`[
			{"key":"COLL1234","data":{"key":"COLL1234","name":"Inbox","parentCollection":false}}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL)
	group := GroupLibrary(42, "Group")
	children, _, err := client.ItemChildren(context.Background(), group, "ABCD1234")
	if err != nil {
		t.Fatalf("ItemChildren: %v", err)
	}
	collections, _, err := client.Collections(context.Background(), group, CollectionsOptions{Top: true})
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if !sawChildren || !sawCollections {
		t.Fatalf("routes not hit: children=%v collections=%v", sawChildren, sawCollections)
	}
	if children[0].Title() != "PDF" {
		t.Errorf("child title = %q", children[0].Title())
	}
	data, err := collections[0].CollectionData()
	if err != nil {
		t.Fatal(err)
	}
	if data.Name != "Inbox" || data.ParentKey() != "" {
		t.Errorf("collection data = %+v parent=%q", data, data.ParentKey())
	}
}

func TestStatusErrorPreservesUnexpectedResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items/MISSING", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := New(srv.URL).Item(context.Background(), UserLibrary(), "MISSING")
	var statusErr StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %T %[1]v, want StatusError", err)
	}
	if statusErr.StatusCode != http.StatusTeapot || !strings.Contains(statusErr.Body, "stout") {
		t.Fatalf("statusErr = %+v", statusErr)
	}
}

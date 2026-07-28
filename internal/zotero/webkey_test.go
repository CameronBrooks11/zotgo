package zotero

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// webFake serves the Web API routes PR2 exercises: /keys/current and a group
// list under the key owner's real user id.
func webFake(t *testing.T, userID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/keys/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Zotero-API-Key") == "" {
			t.Errorf("/keys/current reached without an API key")
		}
		_, _ = w.Write([]byte(`{"userID":` + userID + `,"username":"ada","access":{"user":{"library":true,"files":true,"notes":true,"write":true},"groups":{"all":{"library":true,"write":false}}}}`))
	})
	mux.HandleFunc("/users/"+userID+"/groups", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":88,"version":3,"meta":{"numItems":4},"data":{"id":88,"name":"Reading Group","description":""}}
		]`))
	})
	return httptest.NewServer(mux)
}

func TestWebKey_ParsesUserIDAndAccess(t *testing.T) {
	srv := webFake(t, "12345")
	defer srv.Close()

	k, err := NewWeb(srv.URL, "s3cret").WebKey(context.Background())
	if err != nil {
		t.Fatalf("WebKey: %v", err)
	}
	if k.UserID != 12345 {
		t.Fatalf("UserID = %d, want 12345", k.UserID)
	}
	if !k.Access.User.Write {
		t.Fatal("Access.User.Write = false, want true")
	}
	if !k.Access.Groups["all"].Library {
		t.Fatal("Access.Groups[all].Library = false, want true")
	}
}

// On web, "me" resolves to the key owner's real numeric id, and that id drives
// the /api-less route.
func TestResolveLibrary_WebMeUsesKeyOwner(t *testing.T) {
	srv := webFake(t, "12345")
	defer srv.Close()
	c := NewWeb(srv.URL, "k")

	lib, err := c.ResolveLibrary(context.Background(), "me")
	if err != nil {
		t.Fatalf("ResolveLibrary(me): %v", err)
	}
	if lib.ID != 12345 || lib.Kind != LibraryKindUser {
		t.Fatalf("lib = %+v, want user 12345", lib)
	}
	if got := c.Profile().LibraryPrefix(lib); got != "/users/12345" {
		t.Fatalf("prefix = %q, want /users/12345", got)
	}
}

func TestResolveLibrary_WebGroupByNameAndID(t *testing.T) {
	srv := webFake(t, "12345")
	defer srv.Close()
	c := NewWeb(srv.URL, "k")

	byName, err := c.ResolveLibrary(context.Background(), "Reading Group")
	if err != nil {
		t.Fatalf("ResolveLibrary(name): %v", err)
	}
	if byName.ID != 88 || byName.Name != "Reading Group" {
		t.Fatalf("byName = %+v", byName)
	}
	byID, err := c.ResolveLibrary(context.Background(), "groups/88")
	if err != nil {
		t.Fatalf("ResolveLibrary(id): %v", err)
	}
	if got := c.Profile().LibraryPrefix(byID); got != "/groups/88" {
		t.Fatalf("group prefix = %q, want /groups/88", got)
	}
}

// The group listing must ride the key owner's real user route, not the local
// "0" sentinel.
func TestGroups_WebUsesRealUserRoute(t *testing.T) {
	var paths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/keys/current", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"userID":777,"access":{}}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := NewWeb(srv.URL, "k").Groups(context.Background()); err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/users/777/groups" {
		t.Fatalf("group route = %v, want [/users/777/groups]", paths)
	}
}

// Local identity resolution must touch no network: the "0" sentinel is a
// constant, so "me" resolves offline.
func TestSelfUser_LocalTouchesNoNetwork(t *testing.T) {
	failing := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network must not be used")
		}),
	}
	c := New("http://localhost:1", WithHTTPClient(failing))
	lib, err := c.ResolveLibrary(context.Background(), "me")
	if err != nil {
		t.Fatalf("local ResolveLibrary(me) hit the network: %v", err)
	}
	if lib.ID != 0 || lib.Kind != LibraryKindUser {
		t.Fatalf("lib = %+v, want user 0", lib)
	}
}

func TestWebKey_InvalidKeyMapsToSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := NewWeb(srv.URL, "bad").WebKey(context.Background())
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("err = %v, want ErrInvalidAPIKey", err)
	}
}

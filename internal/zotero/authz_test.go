package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// recordingAuthorizer captures the (op, lib) it was asked about and returns a
// fixed verdict, so a test can assert both what the write path declared and that
// the decision was honored.
type recordingAuthorizer struct {
	err    error
	gotOp  Operation
	gotLib LibraryRef
	calls  int
}

func (r *recordingAuthorizer) AuthorizeWrite(op Operation, lib LibraryRef) error {
	r.calls++
	r.gotOp, r.gotLib = op, lib
	return r.err
}

// A client with no authorizer denies every write, and does so before any network
// I/O — a denial must be free and must not touch Zotero.
func TestWrite_DenyByDefaultNoAuthorizer(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		_, _ = w.Write([]byte(`{"successful":{},"unchanged":{},"failed":{}}`))
	}))
	defer srv.Close()

	c := New(srv.URL) // no SetWriteAuthorizer: deny-by-default
	c.SetLocalKey("K")
	primeServerID(c)
	_, err := c.CreateItems(context.Background(), OpItemCreate, UserLibrary(), []json.RawMessage{json.RawMessage(`{"itemType":"book"}`)})
	if !errors.Is(err, ErrWriteNotAuthorized) {
		t.Fatalf("err = %v, want ErrWriteNotAuthorized", err)
	}
	if hit.Load() {
		t.Error("a denied write reached the server; it must fail closed before any network I/O")
	}
}

// The write path declares the operation and library the caller named, and the
// authorizer's refusal is surfaced unchanged.
func TestWrite_AuthorizerSeesOperationAndLibrary(t *testing.T) {
	sentinel := errors.New("nope")
	group := GroupLibrary(12345, "Team")
	cases := []struct {
		name   string
		wantOp Operation
		call   func(c *Client) error
	}{
		{"item.create", OpItemCreate, func(c *Client) error {
			_, err := c.CreateItems(context.Background(), OpItemCreate, group, []json.RawMessage{json.RawMessage(`{"itemType":"book"}`)})
			return err
		}},
		{"item.patch", OpItemPatch, func(c *Client) error {
			return c.PatchItem(context.Background(), OpItemPatch, group, "AAAA1111", json.RawMessage(`{}`), 1)
		}},
		{"item.replace", OpItemReplace, func(c *Client) error {
			return c.ReplaceItem(context.Background(), OpItemReplace, group, "AAAA1111", json.RawMessage(`{}`), 1)
		}},
		{"item.delete", OpItemDelete, func(c *Client) error {
			return c.DeleteItems(context.Background(), OpItemDelete, group, []string{"AAAA1111"}, 1)
		}},
		{"tag.add", OpTagAdd, func(c *Client) error {
			return c.PatchItem(context.Background(), OpTagAdd, group, "AAAA1111", json.RawMessage(`{}`), 1)
		}},
		{"tag.remove", OpTagRemove, func(c *Client) error {
			return c.PatchItem(context.Background(), OpTagRemove, group, "AAAA1111", json.RawMessage(`{}`), 1)
		}},
		{"tag.delete", OpTagDelete, func(c *Client) error {
			return c.DeleteTags(context.Background(), OpTagDelete, group, []string{"todo"}, 1)
		}},
		{"collection.create", OpCollectionCreate, func(c *Client) error {
			_, err := c.CreateCollections(context.Background(), OpCollectionCreate, group, []json.RawMessage{json.RawMessage(`{"name":"X"}`)})
			return err
		}},
		{"collection.rename", OpCollectionRename, func(c *Client) error {
			return c.PatchCollection(context.Background(), OpCollectionRename, group, "COLL0001", json.RawMessage(`{}`), 1)
		}},
		{"collection.move", OpCollectionMove, func(c *Client) error {
			return c.PatchCollection(context.Background(), OpCollectionMove, group, "COLL0001", json.RawMessage(`{}`), 1)
		}},
		{"collection.delete", OpCollectionDelete, func(c *Client) error {
			return c.DeleteCollections(context.Background(), OpCollectionDelete, group, []string{"COLL0001"}, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingAuthorizer{err: sentinel}
			c := New("http://127.0.0.1:0") // denial precedes any network, so the URL is never dialed
			c.SetWriteAuthorizer(rec)
			if err := tc.call(c); !errors.Is(err, sentinel) {
				t.Fatalf("err = %v, want the authorizer's sentinel", err)
			}
			if rec.calls != 1 {
				t.Fatalf("authorizer consulted %d times, want 1", rec.calls)
			}
			if rec.gotOp != tc.wantOp {
				t.Errorf("operation = %q, want %q", rec.gotOp, tc.wantOp)
			}
			if rec.gotLib != group {
				t.Errorf("library = %+v, want %+v", rec.gotLib, group)
			}
		})
	}
}

// The keyless mint path (/api/local/authorize) is how write authority is first
// obtained, so it must run even with no authorizer installed (deny-by-default).
func TestAuthorize_ExemptFromWriteAuthorizer(t *testing.T) {
	srv, _ := authorizeFake(t, true)
	defer srv.Close()

	c := New(srv.URL) // deny-by-default: no authorizer
	remember, err := c.Authorize(context.Background(), "zotgo")
	if err != nil {
		t.Fatalf("Authorize denied despite the mint path being exempt: %v", err)
	}
	if !remember || !c.HasLocalKey() {
		t.Errorf("authorize did not store the key (remember=%v, hasKey=%v)", remember, c.HasLocalKey())
	}
}

// SetWriteAuthorizer(nil) restores deny-by-default after a permissive stance.
func TestSetWriteAuthorizer_NilRestoresDeny(t *testing.T) {
	c := New("http://127.0.0.1:0")
	c.SetWriteAuthorizer(AllowAllWrites())
	c.SetWriteAuthorizer(nil)
	err := c.DeleteItems(context.Background(), OpItemDelete, UserLibrary(), []string{"AAAA1111"}, 1)
	if !errors.Is(err, ErrWriteNotAuthorized) {
		t.Fatalf("err = %v, want ErrWriteNotAuthorized after clearing the authorizer", err)
	}
}

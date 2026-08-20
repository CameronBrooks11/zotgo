//go:build live

// Live web tests exercise the client against the real hosted Web API at
// api.zotero.org — the one thing the httptest fakes cannot falsify, since they
// encode our reading of that API rather than observe it.
//
//	ZOTGO_API_KEY=<read-scoped key> go test -tags live ./internal/zotero -run TestLiveWeb -v
//
// A read-only key is sufficient; the suite issues no writes. It skips only when
// no key is set (a legitimate absence, e.g. CI) — a present-but-rejected key
// fails loudly rather than skipping, per "a skip is not a pass".
package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
)

func liveWebClient(t *testing.T) (*Client, LibraryRef) {
	t.Helper()
	key := os.Getenv("ZOTGO_API_KEY")
	if key == "" {
		t.Skip("ZOTGO_API_KEY not set; skipping the live web suite")
	}
	c := NewWeb("", key)
	ctx := context.Background()

	h := c.CheckHealth(ctx)
	if !h.Reachable {
		t.Skipf("api.zotero.org unreachable (network, not a code fault): %+v", h)
	}
	if !h.KeyValid {
		t.Fatal("ZOTGO_API_KEY was rejected by api.zotero.org")
	}
	me, err := c.ResolveLibrary(ctx, "me")
	if err != nil {
		t.Fatalf("resolve me: %v", err)
	}
	return c, me
}

// webSampleKeys returns the keys of n top-level items from the resolved library.
func webSampleKeys(t *testing.T, c *Client, lib LibraryRef, n int) []string {
	t.Helper()
	items, _, err := c.Items(context.Background(), lib, ItemsOptions{Top: true, Limit: n})
	if err != nil {
		t.Fatalf("sampling items: %v", err)
	}
	if len(items) < n {
		t.Skipf("need %d top-level items, have %d", n, len(items))
	}
	keys := make([]string, 0, n)
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	return keys
}

// The key owner's library resolves to a real numeric id on the /api-less route,
// and reads return real items — the whole point of the profile.
func TestLiveWebIdentityAndReads(t *testing.T) {
	c, me := liveWebClient(t)
	ctx := context.Background()

	if me.Kind != LibraryKindUser || me.ID == 0 {
		t.Fatalf("me resolved to %+v, want a user library with a real id", me)
	}
	if got := c.Profile().LibraryPrefix(me); got == "/api/users/0" || got[:6] != "/users" {
		t.Fatalf("web library prefix = %q, want /users/<id> (no /api)", got)
	}

	items, page, err := c.Items(ctx, me, ItemsOptions{Top: true, Limit: 3})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if page.TotalResults == 0 || len(items) == 0 {
		t.Fatalf("expected items, got total=%d len=%d", page.TotalResults, len(items))
	}
	for _, it := range items {
		if it.Key == "" || it.ItemType() == "" {
			t.Errorf("item missing key/type: %+v", it)
		}
	}
	t.Logf("web My Library (user %d): %d items total, first=%q (%s)",
		me.ID, page.TotalResults, items[0].Title(), items[0].ItemType())
}

// doctor's web capabilities are derived from the key's own grants, not guessed.
func TestLiveWebDoctorIsProbeDerived(t *testing.T) {
	c, _ := liveWebClient(t)
	h := c.CheckHealth(context.Background())

	if !h.Supports(CapabilityRead) {
		t.Error("a valid key with library access should support read")
	}
	// Connector ingestion and local-file access have no Web API equivalent.
	for _, cap := range []Capability{CapabilityConnectorIngest, CapabilityLocalFileAccess} {
		if h.Supports(cap) {
			t.Errorf("%s should be unsupported on the Web API", cap)
		}
	}
	// Every unsupported capability must explain itself.
	for _, s := range h.Capabilities() {
		if !s.Supported && s.Reason == "" {
			t.Errorf("capability %q unsupported without a reason", s.Name)
		}
	}
	t.Logf("web capabilities: %+v", h.Capabilities())
}

func TestLiveWebCollectionPaths(t *testing.T) {
	client, library := liveWebClient(t)
	assertLiveCollectionPath(t, client, library)
}

func TestLiveWebNotFound(t *testing.T) {
	c, me := liveWebClient(t)
	if _, err := c.Item(context.Background(), me, "ZZZZZZZZ"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Every advertised export format must work over the Web API and produce valid
// output. The Web API returns an itemKey-scoped query in a single page (it does
// not paginate csljson the way the Local API does), so this checks each format's
// per-page shape — the one thing that differs from local, and the reason csljson
// needed unwrapping.
func TestLiveWebExportEveryFormat(t *testing.T) {
	c, me := liveWebClient(t)
	keys := webSampleKeys(t, c, me, 3)
	opts := ItemsOptions{Top: true, ItemKeys: keys, Limit: 100}

	for _, format := range ExportFormats() {
		t.Run(format, func(t *testing.T) {
			out, err := c.Export(context.Background(), me, opts, format)
			if err != nil {
				t.Fatalf("Export(%s) over web: %v", format, err)
			}
			if len(bytes.TrimSpace(out)) == 0 {
				t.Fatalf("format %q produced empty output", format)
			}
		})
	}
}

// csljson must merge to one bare array over the Web API, unwrapping its
// {"items":[…]} envelope — the divergence the fakes could not surface.
func TestLiveWebCSLJSONUnwrapsToBareArray(t *testing.T) {
	c, me := liveWebClient(t)
	keys := webSampleKeys(t, c, me, 3)

	raw, err := c.Export(context.Background(), me, ItemsOptions{Top: true, ItemKeys: keys, Limit: 100}, "csljson")
	if err != nil {
		t.Fatalf("csljson export: %v", err)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("csljson is not one merged bare array: %v", err)
	}
	if len(arr) != len(keys) {
		t.Errorf("csljson items = %d, want %d", len(arr), len(keys))
	}
}

// Record formats over web must carry every requested record.
func TestLiveWebRecordFormats(t *testing.T) {
	c, me := liveWebClient(t)
	keys := webSampleKeys(t, c, me, 3)

	for _, tc := range []struct{ format, prefix string }{
		{"bibtex", "@"},
		{"ris", "TY  - "},
	} {
		t.Run(tc.format, func(t *testing.T) {
			out, err := c.Export(context.Background(), me, ItemsOptions{Top: true, ItemKeys: keys, Limit: 100}, tc.format)
			if err != nil {
				t.Fatalf("Export(%s): %v", tc.format, err)
			}
			if got := countLinesWithPrefix(out, tc.prefix); got != len(keys) {
				t.Errorf("%s: %d records, want %d", tc.format, got, len(keys))
			}
		})
	}
}

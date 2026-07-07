//go:build live

// Live tests exercise the SDK against a real, running Zotero with the Local API
// enabled. They are excluded from normal builds and CI; run them locally with:
//
//	go test -tags live ./internal/zotero -run TestLive -v
//
// Override the target with ZOTGO_BASE_URL if Zotero is not on the default port.
package zotero

import (
	"context"
	"os"
	"testing"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	c := New(os.Getenv("ZOTGO_BASE_URL"))
	if h := c.CheckHealth(context.Background()); !h.Ready() {
		t.Skipf("Zotero not ready for live tests: %+v", h)
	}
	return c
}

func TestLiveUserLibraryReads(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	items, page, err := c.Items(ctx, UserLibrary(), ItemsOptions{Top: true, Limit: 3})
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
	t.Logf("user library: %d items total, first=%q (%s)", page.TotalResults, items[0].Title(), items[0].ItemType())
}

func TestLiveGroupResolutionAndReads(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	groups, err := c.Groups(ctx)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if len(groups) == 0 {
		t.Skip("no group libraries to exercise")
	}
	g := groups[0]

	byName, err := c.ResolveLibrary(ctx, g.Data.Name)
	if err != nil {
		t.Fatalf("ResolveLibrary(%q): %v", g.Data.Name, err)
	}
	if byName.Kind != LibraryKindGroup || byName.ID != g.ID {
		t.Fatalf("name resolution mismatch: %+v vs group %d", byName, g.ID)
	}
	// The routed read must land in the group library, not My Library.
	items, page, err := c.Items(ctx, byName, ItemsOptions{Top: true, Limit: 1})
	if err != nil {
		t.Fatalf("group Items: %v", err)
	}
	for _, it := range items {
		if it.Library.Type != LibraryKindGroup || it.Library.ID != g.ID {
			t.Fatalf("item routed to wrong library: %+v (want group %d)", it.Library, g.ID)
		}
	}
	t.Logf("group %q (id %d): %d items total", g.Data.Name, g.ID, page.TotalResults)
}

func TestLiveNotFound(t *testing.T) {
	c := liveClient(t)
	if _, err := c.Item(context.Background(), UserLibrary(), "ZZZZZZZZ"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

//go:build live

// Live write tests exercise the local write API (zotero/zotero#5015) against a
// real, write-capable Zotero — the only thing that can confirm the merged
// contract matches our reading of it. They mutate the library, so point them at
// a THROWAWAY profile, never your real one:
//
//	ZOTGO_BASE_URL=http://localhost:23119 go test -tags live ./internal/zotero -run TestLiveWrite -v
//
// Authorize pops a modal in Zotero. Click **Always Allow** — a single-use
// ("Allow") key is consumed by the first write, and the round-trip does three.
package zotero

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func liveWriteClient(t *testing.T) (*Client, LibraryRef) {
	t.Helper()
	c := New(os.Getenv("ZOTGO_BASE_URL"))
	h := c.CheckHealth(context.Background())
	if !h.Ready() {
		t.Skipf("Zotero not ready: %+v", h)
	}
	if !h.Supports(CapabilityWrite) {
		t.Skip("this Zotero build has no local write API; run against a write-capable build (beta/main)")
	}
	return c, UserLibrary()
}

// libraryVersion reads the library's current clientVersion from a list
// response's Last-Modified-Version header for bulk writes and deletes.
func libraryVersion(t *testing.T, c *Client, lib LibraryRef) int {
	t.Helper()
	_, page, err := c.Items(context.Background(), lib, ItemsOptions{Limit: 1})
	if err != nil {
		t.Fatalf("reading library version: %v", err)
	}
	v, err := strconv.Atoi(page.LastModifiedVersion)
	if err != nil {
		t.Fatalf("Last-Modified-Version %q is not a number: %v", page.LastModifiedVersion, err)
	}
	return v
}

// TestLiveWriteRoundTrip drives one item through its whole life: authorize,
// create, read back, patch, read back, delete, confirm gone. If any step
// diverges from the merged contract, this is where it surfaces.
func TestLiveWriteRoundTrip(t *testing.T) {
	c, lib := liveWriteClient(t)
	ctx := context.Background()

	t.Log("Authorizing — click **Always Allow** in Zotero's prompt…")
	remember, err := c.Authorize(ctx, "zotgo live write test")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !remember {
		t.Fatal("got a single-use key (you clicked \"Allow\"); this round-trip needs a persistent key — re-run and click \"Always Allow\"")
	}

	// Create.
	item := json.RawMessage(`{"itemType":"book","title":"zotgo live write test"}`)
	res, err := c.CreateItems(ctx, lib, []json.RawMessage{item})
	if err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	if !res.Ok() {
		t.Fatalf("create failed: %+v", res.FirstFailure())
	}
	created := res.Successful["0"]
	if created.Key == "" {
		t.Fatalf("create returned no key: %+v", res)
	}
	t.Logf("created %s (version %d)", created.Key, created.Version)

	// Read back.
	got, err := c.Item(ctx, lib, created.Key)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Title() != "zotgo live write test" {
		t.Errorf("created title = %q, want the one we sent", got.Title())
	}

	// Patch, guarded by the object's current version.
	if err := c.PatchItem(ctx, lib, created.Key, json.RawMessage(`{"title":"zotgo patched"}`), got.Version); err != nil {
		t.Fatalf("PatchItem: %v", err)
	}
	got, err = c.Item(ctx, lib, created.Key)
	if err != nil {
		t.Fatalf("read after patch: %v", err)
	}
	if got.Title() != "zotgo patched" {
		t.Errorf("patched title = %q, want zotgo patched", got.Title())
	}

	// Delete, then confirm it is gone (also cleans up the test item).
	if err := c.DeleteItems(ctx, lib, []string{created.Key}, libraryVersion(t, c, lib)); err != nil {
		t.Fatalf("DeleteItems: %v", err)
	}
	if _, err := c.Item(ctx, lib, created.Key); err != ErrNotFound {
		t.Errorf("item still present after delete: err = %v", err)
	}

	// Collections: create, rename, delete under the same authorization.
	cres, err := c.CreateCollections(ctx, lib, []json.RawMessage{json.RawMessage(`{"name":"zotgo live coll","parentCollection":false}`)})
	if err != nil {
		t.Fatalf("CreateCollections: %v", err)
	}
	if !cres.Ok() {
		t.Fatalf("collection create failed: %+v", cres.FirstFailure())
	}
	ck := cres.Successful["0"].Key
	createdCollection := cres.Successful["0"]
	if err := c.PatchCollection(ctx, lib, ck, json.RawMessage(`{"name":"zotgo live renamed"}`), createdCollection.Version); err != nil {
		t.Fatalf("PatchCollection: %v", err)
	}
	col, err := c.Collection(ctx, lib, ck)
	if err != nil {
		t.Fatalf("read collection: %v", err)
	}
	if data, _ := col.CollectionData(); data.Name != "zotgo live renamed" {
		t.Errorf("renamed collection name = %q, want zotgo live renamed", data.Name)
	}
	if err := c.DeleteCollections(ctx, lib, []string{ck}, libraryVersion(t, c, lib)); err != nil {
		t.Fatalf("DeleteCollections: %v", err)
	}

	// Tags: create a tagged item, strip the tag library-wide, confirm it's gone.
	tres, err := c.CreateItems(ctx, lib, []json.RawMessage{json.RawMessage(`{"itemType":"book","title":"zotgo live tag","tags":[{"tag":"zotgolivetag"}]}`)})
	if err != nil {
		t.Fatalf("create tagged item: %v", err)
	}
	tk := tres.Successful["0"].Key
	if err := c.DeleteTags(ctx, lib, []string{"zotgolivetag"}, libraryVersion(t, c, lib)); err != nil {
		t.Fatalf("DeleteTags: %v", err)
	}
	after, err := c.Item(ctx, lib, tk)
	if err != nil {
		t.Fatalf("read after tag delete: %v", err)
	}
	if d, _ := after.ItemData(); len(d.Tags) != 0 {
		t.Errorf("tag survived library-wide delete: %+v", d.Tags)
	}
	_ = c.DeleteItems(ctx, lib, []string{tk}, libraryVersion(t, c, lib)) // cleanup
}

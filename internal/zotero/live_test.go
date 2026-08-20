//go:build live

// Live tests exercise the client against a real, running Zotero with the Local API
// enabled. They are excluded from normal builds and CI; run them locally with:
//
//	go test -tags live ./internal/zotero -run TestLive -v
//
// Override the target with ZOTGO_BASE_URL if Zotero is not on the default port.
package zotero

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestLiveRelationsMatchItemData(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()
	items, _, err := client.Items(ctx, UserLibrary(), ItemsOptions{Limit: 100})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	for _, item := range items {
		var data struct {
			Relations map[string]json.RawMessage `json:"relations"`
		}
		if err := json.Unmarshal(item.Data, &data); err != nil || len(data.Relations) == 0 {
			continue
		}
		raw, err := client.RawItem(ctx, UserLibrary(), item.Key)
		if err != nil {
			t.Fatalf("RawItem(%s): %v", item.Key, err)
		}
		relations, err := DecodeRelations(raw)
		if err != nil {
			t.Fatalf("DecodeRelations(%s): %v", item.Key, err)
		}
		want := 0
		for predicate, rawTargets := range data.Relations {
			var targets []string
			if err := json.Unmarshal(rawTargets, &targets); err != nil {
				t.Fatalf("item %s relation %q is not an array of strings: %v", item.Key, predicate, err)
			}
			want += len(targets)
		}
		if len(relations) != want {
			t.Fatalf("item %s: decoded %d relations, want %d from item data", item.Key, len(relations), want)
		}
		for _, relation := range relations {
			if relation.Predicate == "" || relation.Target == "" {
				t.Fatalf("item %s: incomplete relation: %#v", item.Key, relation)
			}
		}
		t.Logf("item %s: checked %d outgoing relations", item.Key, len(relations))
		return
	}
	t.Skip("no relations in the first 100 user-library items")
}

func TestLiveCollectionPathsMatchCollectionData(t *testing.T) {
	assertLiveCollectionPath(t, liveClient(t), UserLibrary())
}

func assertLiveCollectionPath(t *testing.T, client *Client, library LibraryRef) {
	t.Helper()
	collections, err := client.AllCollections(context.Background(), library, CollectionsOptions{})
	if err != nil {
		t.Fatalf("AllCollections: %v", err)
	}
	if len(collections) == 0 {
		t.Skip("no collections to exercise")
	}
	type collectionData struct {
		Key              string          `json:"key"`
		Name             string          `json:"name"`
		ParentCollection json.RawMessage `json:"parentCollection"`
	}
	byKey := make(map[string]collectionData, len(collections))
	requested := collections[0].Key
	for _, envelope := range collections {
		var data collectionData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatalf("decode collection %s independently: %v", envelope.Key, err)
		}
		byKey[envelope.Key] = data
		var parent string
		if json.Unmarshal(data.ParentCollection, &parent) == nil && parent != "" {
			requested = envelope.Key
		}
	}
	paths, err := ResolveCollectionPaths(collections, []string{requested})
	if err != nil {
		t.Fatalf("ResolveCollectionPaths(%s): %v", requested, err)
	}
	if len(paths) != 1 || len(paths[0].Segments) == 0 || paths[0].Segments[len(paths[0].Segments)-1].Key != requested {
		t.Fatalf("path = %#v", paths)
	}
	for i, segment := range paths[0].Segments {
		data, ok := byKey[segment.Key]
		if !ok || data.Name != segment.Name {
			t.Fatalf("segment %d does not match independent collection data: %#v", i, segment)
		}
		if i == 0 {
			if !bytes.Equal(bytes.TrimSpace(data.ParentCollection), []byte("false")) {
				t.Fatalf("first segment %q is not a root: parent=%s", segment.Key, data.ParentCollection)
			}
			continue
		}
		var parent string
		if err := json.Unmarshal(data.ParentCollection, &parent); err != nil || parent != paths[0].Segments[i-1].Key {
			t.Fatalf("segment %q parent = %q/%v, want %q", segment.Key, parent, err, paths[0].Segments[i-1].Key)
		}
	}
	t.Logf("collection %s: checked %d path segments", requested, len(paths[0].Segments))
}

func TestLiveNotFound(t *testing.T) {
	c := liveClient(t)
	if _, err := c.Item(context.Background(), UserLibrary(), "ZZZZZZZZ"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

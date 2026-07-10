//go:build live

// Live DTO tests check the field mapping against a real Zotero library. The
// httptest fakes are seeded from shapes we captured and therefore encode our own
// reading of the API; they cannot tell us the reading is wrong.
//
// Every assertion here decodes Zotero's raw JSON independently and compares it to
// the DTO, so a mis-keyed field (itemType -> type, tag type 1 -> automatic) fails
// rather than agreeing with itself.
//
//	go test -tags live ./internal/output -run TestLive -v
package output

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func liveClient(t *testing.T) *zotero.Client {
	t.Helper()
	c := zotero.New(os.Getenv("ZOTGO_BASE_URL"))
	if h := c.CheckHealth(context.Background()); !h.Ready() {
		t.Skipf("Zotero not ready for live tests: %+v", h)
	}
	return c
}

func liveItems(t *testing.T, c *zotero.Client, limit int) []zotero.Envelope {
	t.Helper()
	items, _, err := c.Items(context.Background(), zotero.UserLibrary(), zotero.ItemsOptions{Top: true, Limit: limit})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) == 0 {
		t.Skip("empty library")
	}
	return items
}

// rawItem decodes an envelope's data payload without going through ItemData, so
// the comparison has an independent source of truth.
func rawItem(t *testing.T, e zotero.Envelope) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Data, &m); err != nil {
		t.Fatalf("decoding raw data for %s: %v", e.Key, err)
	}
	return m
}

func TestLiveItemDTOMapsZoteroFields(t *testing.T) {
	c := liveClient(t)

	for _, e := range liveItems(t, c, 25) {
		raw := rawItem(t, e)
		dto := NewItem(e)

		if dto.Key != e.Key {
			t.Errorf("%s: key = %q", e.Key, dto.Key)
		}
		// The DTO renames Zotero's itemType to type.
		if want, _ := raw["itemType"].(string); dto.Type != want {
			t.Errorf("%s: type = %q, want itemType %q", e.Key, dto.Type, want)
		}
		if want, _ := raw["title"].(string); dto.Title != want {
			t.Errorf("%s: title = %q, want %q", e.Key, dto.Title, want)
		}
		if want, _ := raw["date"].(string); dto.Date != want {
			t.Errorf("%s: date = %q, want %q", e.Key, dto.Date, want)
		}

		if rawCreators, ok := raw["creators"].([]any); ok {
			if len(dto.Creators) != len(rawCreators) {
				t.Errorf("%s: %d creators, want %d", e.Key, len(dto.Creators), len(rawCreators))
			}
		} else if len(dto.Creators) != 0 {
			t.Errorf("%s: invented %d creators", e.Key, len(dto.Creators))
		}

		// A creator is first/last OR a single name — never both, never neither.
		for i, cr := range dto.Creators {
			hasPair := cr.FirstName != "" || cr.LastName != ""
			hasName := cr.Name != ""
			if hasPair == hasName {
				t.Errorf("%s: creator %d has both or neither name form: %+v", e.Key, i, cr)
			}
			if cr.Type == "" {
				t.Errorf("%s: creator %d has no type", e.Key, i)
			}
		}
	}
}

// The automatic flag is derived from Zotero's numeric tag type. Confirm the
// mapping against the raw value rather than against our own constant.
func TestLiveTagAutomaticFlagMatchesZoteroType(t *testing.T) {
	c := liveClient(t)

	var checked int
	for _, e := range liveItems(t, c, 50) {
		raw := rawItem(t, e)
		rawTags, _ := raw["tags"].([]any)
		dto := NewItem(e)

		if len(dto.Tags) != len(rawTags) {
			t.Fatalf("%s: %d tags, want %d", e.Key, len(dto.Tags), len(rawTags))
		}
		for i, rt := range rawTags {
			m, ok := rt.(map[string]any)
			if !ok {
				t.Fatalf("%s: tag %d is not an object", e.Key, i)
			}
			name, _ := m["tag"].(string)
			// Zotero omits type for manual tags, or sends 0.
			typ, _ := m["type"].(float64)

			if dto.Tags[i].Name != name {
				t.Errorf("%s: tag %d name = %q, want %q", e.Key, i, dto.Tags[i].Name, name)
			}
			if want := typ == 1; dto.Tags[i].Automatic != want {
				t.Errorf("%s: tag %q automatic = %v, want %v (raw type %v)",
					e.Key, name, dto.Tags[i].Automatic, want, typ)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Skip("no tags in the sampled items")
	}
	t.Logf("checked %d tags", checked)
}

// numChildren and creatorSummary live in the envelope's meta, not its data.
func TestLiveItemDTOMapsMetaFields(t *testing.T) {
	c := liveClient(t)

	for _, e := range liveItems(t, c, 25) {
		dto := NewItem(e)

		var meta struct {
			NumChildren    *int    `json:"numChildren"`
			CreatorSummary *string `json:"creatorSummary"`
			ParsedDate     *string `json:"parsedDate"`
		}
		blob, err := json.Marshal(e.Meta)
		if err != nil {
			t.Fatalf("%s: re-encoding meta: %v", e.Key, err)
		}
		if err := json.Unmarshal(blob, &meta); err != nil {
			t.Fatalf("%s: decoding meta: %v", e.Key, err)
		}

		if meta.NumChildren != nil && dto.NumChildren != *meta.NumChildren {
			t.Errorf("%s: numChildren = %d, want %d", e.Key, dto.NumChildren, *meta.NumChildren)
		}
		if meta.CreatorSummary != nil && dto.CreatorSummary != *meta.CreatorSummary {
			t.Errorf("%s: creatorSummary = %q, want %q", e.Key, dto.CreatorSummary, *meta.CreatorSummary)
		}
		if meta.ParsedDate != nil && dto.ParsedDate != *meta.ParsedDate {
			t.Errorf("%s: parsedDate = %q, want %q", e.Key, dto.ParsedDate, *meta.ParsedDate)
		}
	}
}

// show nests children under the item; every child must be a real attachment or
// note, and the count must agree with the parent's numChildren.
func TestLiveItemWithChildren(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	for _, e := range liveItems(t, c, 25) {
		if NewItem(e).NumChildren == 0 {
			continue
		}
		children, _, err := c.ItemChildren(ctx, zotero.UserLibrary(), e.Key)
		if err != nil {
			t.Fatalf("ItemChildren(%s): %v", e.Key, err)
		}
		dto := NewItemWithChildren(e, children)
		if len(dto.Children) != len(children) {
			t.Fatalf("%s: %d children in DTO, want %d", e.Key, len(dto.Children), len(children))
		}
		for _, ch := range dto.Children {
			if ch.Type != "attachment" && ch.Type != "note" && ch.Type != "annotation" {
				t.Errorf("%s: unexpected child type %q", e.Key, ch.Type)
			}
		}
		t.Logf("item %s has %d children", e.Key, len(dto.Children))
		return // One item with children is enough to exercise the path.
	}
	t.Skip("no items with children in the sample")
}

// The DTO must never carry a nil slice into JSON: scripts iterate these fields.
func TestLiveDTOSlicesMarshalAsArrays(t *testing.T) {
	c := liveClient(t)

	for _, e := range liveItems(t, c, 50) {
		blob, err := json.Marshal(NewItem(e))
		if err != nil {
			t.Fatalf("%s: Marshal: %v", e.Key, err)
		}
		var probe struct {
			Creators    *[]Creator `json:"creators"`
			Tags        *[]Tag     `json:"tags"`
			Collections *[]string  `json:"collections"`
		}
		if err := json.Unmarshal(blob, &probe); err != nil {
			t.Fatalf("%s: Unmarshal: %v", e.Key, err)
		}
		if probe.Creators == nil || probe.Tags == nil || probe.Collections == nil {
			t.Fatalf("%s: a slice field marshalled as null: %s", e.Key, blob)
		}
	}
}

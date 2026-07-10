package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func item(key, itemType, title string) zotero.Envelope {
	data, _ := json.Marshal(map[string]any{"key": key, "itemType": itemType, "title": title})
	return zotero.Envelope{Key: key, Data: data}
}

func collection(key, name, parent string) zotero.Envelope {
	m := map[string]any{"key": key, "name": name}
	if parent == "" {
		m["parentCollection"] = false
	} else {
		m["parentCollection"] = parent
	}
	data, _ := json.Marshal(m)
	return zotero.Envelope{Key: key, Data: data}
}

func TestItemsTable(t *testing.T) {
	var buf bytes.Buffer
	Items(&buf, []zotero.Envelope{item("HRAC4E44", "journalArticle", "Algae paper")})
	out := buf.String()
	for _, want := range []string{"KEY", "TYPE", "TITLE", "HRAC4E44", "journalArticle", "Algae paper"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestCollectionsTree(t *testing.T) {
	var buf bytes.Buffer
	Collections(&buf, []zotero.Envelope{
		collection("ROOT", "Research", ""),
		collection("CHILD", "Subtopic", "ROOT"),
		collection("ORPHAN", "Detached", "MISSINGPARENT"),
	}, false)
	out := buf.String()

	// Child must be indented under its parent.
	rootIdx := strings.Index(out, "Research")
	childLine := lineContaining(out, "Subtopic")
	if rootIdx < 0 || childLine == "" {
		t.Fatalf("tree missing nodes:\n%s", out)
	}
	if !strings.HasPrefix(childLine, "  ") {
		t.Errorf("child not indented: %q", childLine)
	}
	// An orphan (parent not in set) is promoted to a root, not dropped.
	if !strings.Contains(out, "Detached") {
		t.Errorf("orphan dropped:\n%s", out)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	// Multibyte input must not be split mid-rune.
	got := truncate("photo‐bioreacteur design principles here", 10)
	if r := []rune(got); len(r) != 10 {
		t.Fatalf("truncate len = %d runes, want 10: %q", len(r), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

func TestItemDetailWithChildren(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"key": "HRAC4E44", "itemType": "journalArticle", "title": "Algae paper",
		"tags": []map[string]any{{"tag": "ml"}, {"tag": "review"}},
	})
	parent := zotero.Envelope{
		Key:  "HRAC4E44",
		Data: data,
		Meta: map[string]json.RawMessage{
			"creatorSummary": json.RawMessage(`"Posten"`),
			"parsedDate":     json.RawMessage(`"2009-06"`),
		},
	}
	children := []zotero.Envelope{item("CHILD001", "attachment", "Full Text PDF")}

	var buf bytes.Buffer
	Item(&buf, parent, children)
	out := buf.String()
	for _, want := range []string{"HRAC4E44", "journalArticle", "Algae paper", "Posten", "2009-06", "ml, review", "Children (1)", "Full Text PDF"} {
		if !strings.Contains(out, want) {
			t.Errorf("item detail missing %q:\n%s", want, out)
		}
	}
}

func TestItemDetailCreatorFallback(t *testing.T) {
	// No meta.creatorSummary: authors come from data.creators.
	data, _ := json.Marshal(map[string]any{
		"key": "X", "itemType": "book", "title": "T",
		"creators": []map[string]any{
			{"creatorType": "author", "firstName": "Ada", "lastName": "Lovelace"},
			{"creatorType": "author", "name": "OpenAI"},
		},
	})
	var buf bytes.Buffer
	Item(&buf, zotero.Envelope{Key: "X", Data: data}, nil)
	out := buf.String()
	if !strings.Contains(out, "Lovelace, Ada") || !strings.Contains(out, "OpenAI") {
		t.Errorf("creator fallback missing:\n%s", out)
	}
}

func TestStatsRender(t *testing.T) {
	var buf bytes.Buffer
	Stats(&buf, "My Library", zotero.Stats{Items: 2203, TopItems: 1093, Collections: 57, Tags: 812})
	out := buf.String()
	for _, want := range []string{"My Library", "2203", "1093", "57", "812"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestCollectionsFlatSorted(t *testing.T) {
	var buf bytes.Buffer
	Collections(&buf, []zotero.Envelope{
		collection("B", "Zebra", ""),
		collection("A", "Apple", ""),
	}, true)
	out := buf.String()
	if strings.Index(out, "Apple") > strings.Index(out, "Zebra") {
		t.Errorf("flat list not sorted by name:\n%s", out)
	}
	if !strings.Contains(out, "KEY") || !strings.Contains(out, "NAME") {
		t.Errorf("flat list missing header:\n%s", out)
	}
}

func lineContaining(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}

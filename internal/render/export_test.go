package render

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := CSV(&buf, []zotero.Envelope{item("AAAA", "journalArticle", "Algae, and other plants")}); err != nil {
		t.Fatal(err)
	}
	// Parse it back: a comma in the title must be quoted, not split.
	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v\n%s", err, buf.String())
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (header + 1)", len(rows))
	}
	if rows[0][0] != "key" || rows[0][2] != "title" {
		t.Errorf("header = %v", rows[0])
	}
	if rows[1][0] != "AAAA" || rows[1][2] != "Algae, and other plants" {
		t.Errorf("row = %v", rows[1])
	}
}

func TestMarkdown(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, []zotero.Envelope{item("AAAA", "book", "My Title")}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"**My Title**", "`AAAA`", "book"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}

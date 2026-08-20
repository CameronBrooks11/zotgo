package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestNoteRendersMetadataAndExactHTML(t *testing.T) {
	note := zotero.Note{
		Key:          "NOTE0001",
		ParentKey:    "PARENT01",
		DateAdded:    "2026-01-01T00:00:00Z",
		DateModified: "2026-01-02T00:00:00Z",
		Tags:         []zotero.Tag{{Tag: "review"}},
		HTML:         `<div data-schema-version="9"><p>Body</p></div>`,
	}
	var output bytes.Buffer
	Note(&output, note)
	for _, want := range []string{"NOTE0001", "PARENT01", "2026-01-01", "2026-01-02", "review", "HTML:", note.HTML} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

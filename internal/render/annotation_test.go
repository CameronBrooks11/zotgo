package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestAnnotations(t *testing.T) {
	var out bytes.Buffer
	Annotations(&out, "ATTACH01", []zotero.Annotation{
		{Key: "ANN00001", Type: "highlight", PageLabel: "12", SortIndex: "00001", Color: "#ffd400", HasText: true},
		{Key: "ANN00002", Type: "note", PageLabel: "13", SortIndex: "00002", Color: "#ff6666", HasComment: true},
	})
	text := out.String()
	for _, want := range []string{"KEY", "PAGE", "SORT INDEX", "TYPE", "TEXT", "COMMENT", "COLOR", "ANN00001", "highlight", "yes", "ANN00002", "2 annotations"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "quoted body") || strings.Contains(text, "comment body") {
		t.Fatalf("output leaked annotation bodies:\n%s", text)
	}
}

func TestAnnotationsEmpty(t *testing.T) {
	var out bytes.Buffer
	Annotations(&out, "ATTACH01", nil)
	if got, want := out.String(), "No annotations for attachment ATTACH01.\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

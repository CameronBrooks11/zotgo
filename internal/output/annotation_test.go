package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestNewAnnotationsStablePrivacyFields(t *testing.T) {
	records := NewAnnotations([]zotero.Annotation{{
		Key: "ANN00001", AttachmentKey: "ATTACH01", Type: "highlight",
		PageLabel: "12", Color: "#ffd400", SortIndex: "00012|00001",
		HasText: true, HasComment: false,
	}})
	if len(records) != 1 || records[0].Key != "ANN00001" || !records[0].HasText || records[0].HasComment {
		t.Fatalf("records = %#v", records)
	}
	encoded, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"annotationText", "annotationComment", "annotationPosition", "image", "version"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("stable annotation leaked %q: %s", forbidden, text)
		}
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"key", "attachmentKey", "type", "pageLabel", "color", "sortIndex", "hasText", "hasComment"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("field %q absent: %s", field, encoded)
		}
	}
	if len(fields) != 8 {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestNewAnnotationsReturnsEmptyArray(t *testing.T) {
	if records := NewAnnotations(nil); records == nil || len(records) != 0 {
		t.Fatalf("records = %#v", records)
	}
}

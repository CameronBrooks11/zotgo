package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// copySource is a minimal Zotero standing in for one item and its tree. It exists
// to make the *shape* of the walk testable: an item's children, and — separately,
// because Zotero will not volunteer them — the annotations under each attachment.
type copySource struct {
	// items maps every key the walk may fetch to its envelope. Annotations are
	// reached by fetching their attachment first, so children have to be
	// individually addressable, not only listed.
	items       map[string]string
	children    string
	annotations map[string]string
}

func (s copySource) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := s.items[r.PathValue("key")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("GET /api/users/0/items/{key}/children", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		// The filter is the whole point: an unfiltered listing never carries
		// annotations, so a walk that forgets it sees an empty set and reports a
		// complete copy (docs/zotero-api.md).
		if r.URL.Query().Get("itemType") == "annotation" {
			body, ok := s.annotations[key]
			if !ok {
				body = "[]"
			}
			_, _ = w.Write([]byte(body))
			return
		}
		if key == "SRC00001" && s.children != "" {
			_, _ = w.Write([]byte(s.children))
			return
		}
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func envelope(key, itemType, body string) string {
	return fmt.Sprintf(`{"key":%q,"version":1,"data":{"key":%q,"itemType":%q%s}}`, key, key, itemType, body)
}

func fullTree() copySource {
	item := envelope("SRC00001", "journalArticle", `,"title":"Scale-up of photobioreactors"`)
	note := envelope("NOTE0001", "note", `,"note":"<p>a note</p>","parentItem":"SRC00001"`)
	// An enclosure link is what marks managed bytes; without it Zotero is saying
	// it has no file, however the linkMode reads.
	managed := `{"key":"ATTA0001","version":1,"links":{"enclosure":{"href":"http://127.0.0.1/file","type":"application/pdf"}},` +
		`"data":{"key":"ATTA0001","itemType":"attachment","title":"Full Text PDF","linkMode":"imported_url","contentType":"application/pdf","parentItem":"SRC00001"}}`
	linked := envelope("ATTB0001", "attachment", `,"title":"Homepage","linkMode":"linked_url","url":"https://example.org","parentItem":"SRC00001"`)
	annotation := envelope("ANNO0001", "annotation", `,"annotationType":"highlight","annotationText":"tensile testing","parentItem":"ATTA0001"`)

	return copySource{
		items: map[string]string{
			"SRC00001": item, "NOTE0001": note, "ATTA0001": managed, "ATTB0001": linked, "ANNO0001": annotation,
		},
		children:    "[" + strings.Join([]string{note, managed, linked}, ",") + "]",
		annotations: map[string]string{"ATTA0001": "[" + annotation + "]"},
	}
}

func planFor(t *testing.T, src copySource, withAttachments bool) plannedItem {
	t.Helper()
	c := zotero.New(src.server(t).URL)
	planned, err := planItemCopy(context.Background(), c, zotero.UserLibrary(), "SRC00001", withAttachments)
	if err != nil {
		t.Fatalf("planItemCopy: %v", err)
	}
	return planned
}

// The walk has to reach annotations, which live a level below the item and are
// absent from an unfiltered children listing. A plan that stops at the item's
// direct children would report a complete copy while dropping every annotation —
// the exact failure this command exists to fix.
func TestPlanReachesAnnotationsUnderAttachments(t *testing.T) {
	planned := planFor(t, fullTree(), true)

	if planned.itemType != "journalArticle" || planned.title == "" {
		t.Fatalf("planned item = %+v", planned)
	}
	if len(planned.children) != 3 {
		t.Fatalf("children = %d, want 3", len(planned.children))
	}

	var annotations int
	for _, child := range planned.children {
		if child.sourceKey == "ATTA0001" {
			annotations = len(child.children)
			if !child.managed {
				t.Error("imported_url attachment with an enclosure should be managed")
			}
		}
		if child.sourceKey == "ATTB0001" && child.managed {
			t.Error("linked_url attachment has no managed bytes")
		}
	}
	if annotations != 1 {
		t.Errorf("annotations under ATTA0001 = %d, want 1", annotations)
	}
}

// --no-attachments skips the attachment, and with it the bytes and the
// annotations underneath. The skip carries a reason because it is reported to the
// user rather than silently applied.
func TestPlanNoAttachmentsSkipsWithAReason(t *testing.T) {
	planned := planFor(t, fullTree(), false)

	for _, child := range planned.children {
		switch child.kind {
		case "attachment":
			if child.skipReason == "" {
				t.Errorf("attachment %s was not skipped under --no-attachments", child.sourceKey)
			}
			if len(child.children) != 0 {
				t.Errorf("attachment %s carried annotations while skipped", child.sourceKey)
			}
		case "note":
			if child.skipReason != "" {
				t.Errorf("note %s was skipped; --no-attachments does not cover notes", child.sourceKey)
			}
		}
	}
}

// The prompt distinguishes carried from skipped. A single total would make a copy
// that drops everything look like one that carries everything.
func TestPlanCountsSeparateCarriedFromSkipped(t *testing.T) {
	carried := copyPlan{items: []plannedItem{planFor(t, fullTree(), true)}}.counts()
	if carried.items != 1 || carried.notes != 1 || carried.attachments != 2 || carried.annotations != 1 {
		t.Errorf("carried counts = %+v", carried)
	}
	if carried.files != 1 {
		t.Errorf("files = %d, want 1 (only the imported_url attachment has bytes)", carried.files)
	}

	skipped := copyPlan{items: []plannedItem{planFor(t, fullTree(), false)}}.counts()
	if skipped.attachments != 0 || skipped.annotations != 0 {
		t.Errorf("skipped plan still counts attachments/annotations: %+v", skipped)
	}
	if skipped.skipped != 2 {
		t.Errorf("skipped = %d, want both attachments", skipped.skipped)
	}
	if skipped.notes != 1 {
		t.Errorf("notes = %d, want the note still carried", skipped.notes)
	}
}

// Copying a child on its own would need a parent in the target that the command
// has no way to name, so it refuses rather than inventing one.
func TestPlanRefusesAChildItem(t *testing.T) {
	src := copySource{items: map[string]string{
		"SRC00001": envelope("SRC00001", "attachment", `,"linkMode":"linked_url"`),
	}}
	c := zotero.New(src.server(t).URL)

	_, err := planItemCopy(context.Background(), c, zotero.UserLibrary(), "SRC00001", true)
	if err == nil || !strings.Contains(err.Error(), "not a top-level item") {
		t.Fatalf("err = %v, want a refusal naming the problem", err)
	}
}

// The data object is Zotero's write vocabulary; the envelope around it is not.
// Planning from the envelope would send key and version back on a create.
func TestPlanCarriesTheDataObjectNotTheEnvelope(t *testing.T) {
	planned := planFor(t, fullTree(), true)

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(planned.data, &fields); err != nil {
		t.Fatalf("plan data is not an object: %v", err)
	}
	if _, present := fields["itemType"]; !present {
		t.Error("planned data has no itemType; the envelope was captured instead of its data")
	}
	if _, present := fields["version"]; present {
		t.Error("planned data carries version; that is envelope metadata, not item data")
	}
}

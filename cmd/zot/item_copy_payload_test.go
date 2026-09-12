package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustJSON encodes a value as a JSON literal for embedding in a fixture.
func mustJSON(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func decodeFields(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("payload is not an object: %v (%s)", err, raw)
	}
	return fields
}

// Every dropped field is dropped for a reason that bites differently: a carried
// key or version collides with the target's own, a carried dateAdded lies about
// when the copy happened, and carried relations or collections point at objects
// that do not exist in the target library.
func TestCopyPayloadDropsSourceAndServerFields(t *testing.T) {
	source := json.RawMessage(`{
		"key": "SRC12345",
		"version": 42,
		"itemType": "journalArticle",
		"title": "Scale-up of photobioreactors",
		"dateAdded": "2020-01-01T00:00:00Z",
		"dateModified": "2021-01-01T00:00:00Z",
		"relations": {"dc:replaces": ["http://zotero.org/users/1/items/OTHER123"]},
		"collections": ["SRCCOLL1"],
		"tags": [{"tag": "algae"}]
	}`)

	payload, err := copyPayload(source, "", nil)
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	fields := decodeFields(t, payload)

	for _, gone := range []string{"key", "version", "dateAdded", "dateModified", "relations", "collections"} {
		if _, present := fields[gone]; present {
			t.Errorf("%q survived the copy; it belongs to the source library or to Zotero", gone)
		}
	}
	for _, kept := range []string{"itemType", "title", "tags"} {
		if _, present := fields[kept]; !present {
			t.Errorf("%q was dropped; it is the content being copied", kept)
		}
	}
}

// A child is reparented onto the item created in the target. Carrying the
// source's parentItem would attach it to an item in the wrong library, or to
// nothing at all.
func TestCopyPayloadReparentsChildren(t *testing.T) {
	source := json.RawMessage(`{"key":"NOTE1234","itemType":"note","note":"<p>hi</p>","parentItem":"SRC12345"}`)

	payload, err := copyPayload(source, "NEWPARENT", nil)
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	var got struct {
		ParentItem string `json:"parentItem"`
		Note       string `json:"note"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ParentItem != "NEWPARENT" {
		t.Errorf("parentItem = %q, want the newly created parent", got.ParentItem)
	}
	if got.Note == "" {
		t.Error("note body was lost")
	}
}

// Zotero assigns filename during upload and refuses it before the attachment has
// a key; md5 and mtime describe bytes that have not been uploaded yet.
func TestCopyPayloadDropsUploadManagedAttachmentFields(t *testing.T) {
	source := json.RawMessage(`{
		"key": "ATT12345", "itemType": "attachment", "linkMode": "imported_url",
		"title": "Full Text PDF", "contentType": "application/pdf",
		"filename": "paper.pdf", "md5": "d41d8cd98f00b204e9800998ecf8427e", "mtime": 1700000000000
	}`)

	payload, err := copyPayload(source, "NEWPARENT", nil)
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	fields := decodeFields(t, payload)

	for _, gone := range []string{"filename", "md5", "mtime"} {
		if _, present := fields[gone]; present {
			t.Errorf("%q survived; the upload protocol assigns it", gone)
		}
	}
	for _, kept := range []string{"linkMode", "contentType", "title"} {
		if _, present := fields[kept]; !present {
			t.Errorf("%q was dropped; the attachment cannot be recreated without it", kept)
		}
	}
}

// annotationPosition and annotationSortIndex are opaque page geometry. They are
// meaningful only against the document they describe, which travels with them, so
// altering or regenerating either would misplace the annotation.
func TestCopyPayloadCarriesAnnotationGeometryVerbatim(t *testing.T) {
	position := `{"pageIndex":5,"rects":[[79.766,180.921,288.693,189.772]]}`
	source := json.RawMessage(`{
		"key": "ANN12345", "itemType": "annotation", "parentItem": "ATT12345",
		"annotationType": "highlight", "annotationText": "tensile testing",
		"annotationColor": "#e56eee", "annotationPageLabel": "69",
		"annotationSortIndex": "00005|000465|00603",
		"annotationPosition": ` + mustJSON(position) + `
	}`)

	payload, err := copyPayload(source, "NEWATTACH", nil)
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["annotationPosition"] != position {
		t.Errorf("annotationPosition = %v, want it carried verbatim", got["annotationPosition"])
	}
	if got["annotationSortIndex"] != "00005|000465|00603" {
		t.Errorf("annotationSortIndex = %v, want it carried verbatim", got["annotationSortIndex"])
	}
	if got["parentItem"] != "NEWATTACH" {
		t.Errorf("parentItem = %v, want the new attachment key", got["parentItem"])
	}
}

// Collections in the source name collections that do not exist in the target, so
// they are replaced rather than carried.
func TestCopyPayloadReplacesCollections(t *testing.T) {
	source := json.RawMessage(`{"key":"SRC1","itemType":"book","collections":["SRCCOLL1","SRCCOLL2"]}`)

	payload, err := copyPayload(source, "", []string{"TARGETCOLL"})
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	var got struct {
		Collections []string `json:"collections"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Collections) != 1 || got.Collections[0] != "TARGETCOLL" {
		t.Errorf("collections = %v, want only the target collection", got.Collections)
	}
}

// Without itemType Zotero rejects the create and names a field the user never
// supplied, so the failure has to happen here where the reason is still legible.
func TestCopyPayloadRequiresItemType(t *testing.T) {
	_, err := copyPayload(json.RawMessage(`{"key":"SRC1","title":"no type"}`), "", nil)
	if err == nil || !strings.Contains(err.Error(), "itemType") {
		t.Fatalf("err = %v, want a refusal naming itemType", err)
	}
}

// Payload bytes are compared in dry-run output and in tests, and Go's map
// iteration order is random, so encoding has to be stable.
func TestCopyPayloadIsStable(t *testing.T) {
	source := json.RawMessage(`{"itemType":"book","title":"A","abstractNote":"B","place":"C","publisher":"D"}`)
	first, err := copyPayload(source, "", nil)
	if err != nil {
		t.Fatalf("copyPayload: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := copyPayload(source, "", nil)
		if err != nil {
			t.Fatalf("copyPayload: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("payload is not stable:\n  %s\n  %s", first, again)
		}
	}
}

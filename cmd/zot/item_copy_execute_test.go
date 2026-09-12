package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// copyTarget records every object written to it, so a test can assert what the
// copy actually sent rather than only what it reported sending.
type copyTarget struct {
	mu      sync.Mutex
	written []map[string]json.RawMessage
	next    int
	// rejectType makes writes of one itemType fail, so a partly-written tree can
	// be exercised. Partial success is the normal case for a copy, not an edge.
	rejectType string
}

func (tg *copyTarget) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var batch []map[string]json.RawMessage
	if err := json.Unmarshal(body, &batch); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tg.mu.Lock()
	successful := map[string]string{}
	failed := map[string]any{}
	for i, object := range batch {
		var itemType string
		if raw, ok := object["itemType"]; ok {
			_ = json.Unmarshal(raw, &itemType)
		}
		if tg.rejectType != "" && itemType == tg.rejectType {
			failed[itoa(i)] = map[string]any{"code": 400, "message": "rejected by the fake"}
			continue
		}
		tg.written = append(tg.written, object)
		tg.next++
		successful[itoa(i)] = "NEWKEY" + itoa(tg.next)
	}
	tg.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"successful": successfulEnvelopes(successful),
		"unchanged":  map[string]string{},
		"failed":     failed,
	})
}

func successfulEnvelopes(keys map[string]string) map[string]any {
	out := map[string]any{}
	for index, key := range keys {
		out[index] = map[string]any{"key": key, "version": 1, "data": map[string]any{"key": key}}
	}
	return out
}

func itoa(i int) string { return string(rune('0' + i%10)) }

func (tg *copyTarget) sent(t *testing.T, itemType string) map[string]json.RawMessage {
	t.Helper()
	tg.mu.Lock()
	defer tg.mu.Unlock()
	for _, object := range tg.written {
		var got string
		if raw, ok := object["itemType"]; ok {
			_ = json.Unmarshal(raw, &got)
		}
		if got == itemType {
			return object
		}
	}
	t.Fatalf("nothing of itemType %q was written; got %d objects", itemType, len(tg.written))
	return nil
}

func stringField(t *testing.T, object map[string]json.RawMessage, name string) string {
	t.Helper()
	var value string
	if raw, ok := object[name]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

// The whole tree has to be written, and each level reparented onto the key the
// level above was assigned. An annotation reparented onto the *item* rather than
// the new attachment would be rejected or silently misfiled.
func TestExecuteCopyWritesTheTreeAndReparents(t *testing.T) {
	src := fullTree()
	target := &copyTarget{}

	c, ctx := writableCopyClient(t, src, target, nil)

	planned, err := planItemCopy(ctx, c, zotero.UserLibrary(), "SRC00001", true)
	if err != nil {
		t.Fatalf("planItemCopy: %v", err)
	}
	record := executeItemCopy(ctx, c, zotero.UserLibrary(), zotero.UserLibrary(), 0, planned, nil)

	if record.Key == "" {
		t.Fatalf("item was not created: %+v", record.Failure)
	}

	item := target.sent(t, "journalArticle")
	if _, present := item["key"]; present {
		t.Error("the source key was sent on a create")
	}
	if _, present := item["collections"]; present {
		t.Error("source collections were sent; they name collections in the wrong library")
	}

	note := target.sent(t, "note")
	if parent := stringField(t, note, "parentItem"); parent != record.Key {
		t.Errorf("note parentItem = %q, want the new item key %q", parent, record.Key)
	}

	// The one that matters: an annotation belongs to the new *attachment*, which
	// is a level below the item and has its own freshly assigned key.
	var attachmentKey string
	for _, child := range record.Children {
		if child.Kind == "attachment" && child.SourceKey == "ATTA0001" {
			attachmentKey = child.Key
			if len(child.Children) != 1 {
				t.Fatalf("annotations under the copied attachment = %d, want 1", len(child.Children))
			}
		}
	}
	if attachmentKey == "" {
		t.Fatal("the managed attachment was not created")
	}
	annotation := target.sent(t, "annotation")
	if parent := stringField(t, annotation, "parentItem"); parent != attachmentKey {
		t.Errorf("annotation parentItem = %q, want the new attachment key %q", parent, attachmentKey)
	}
	if stringField(t, annotation, "annotationText") == "" {
		t.Error("annotation text was lost in the copy")
	}
}

// An attachment Zotero has no bytes for is an ordinary outcome — the metadata
// copies and the file reports skipped, rather than the copy failing.
func TestExecuteCopyReportsAFilelessAttachmentAsSkipped(t *testing.T) {
	src := fullTree()
	target := &copyTarget{}

	// Zotero's answer when an item has no file to point at.
	noFile := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Not a file attachment: " + r.PathValue("key")))
	}
	c, ctx := writableCopyClient(t, src, target, noFile)

	planned, err := planItemCopy(ctx, c, zotero.UserLibrary(), "SRC00001", true)
	if err != nil {
		t.Fatalf("planItemCopy: %v", err)
	}
	record := executeItemCopy(ctx, c, zotero.UserLibrary(), zotero.UserLibrary(), 0, planned, nil)

	for _, child := range record.Children {
		if child.SourceKey != "ATTA0001" {
			continue
		}
		if child.Status == output.StatusFailed {
			t.Errorf("attachment reported failed; a missing file is not a failure: %+v", child.Failure)
		}
		if child.File == nil || child.File.Status != output.StatusSkipped {
			t.Errorf("file status = %+v, want skipped", child.File)
		}
	}
}

// writableCopyClient wires a source tree to a target that accepts writes. The
// write path needs more than an endpoint: Zotero echoes a server id on every
// response and refuses a write without it, and the client mints its key through
// the authorize route rather than being handed one.
func writableCopyClient(t *testing.T, src copySource, target *copyTarget, fileURL http.HandlerFunc) (*zotero.Client, context.Context) {
	t.Helper()
	const serverID = "SERVERID1234"
	setServerID := func(w http.ResponseWriter) { w.Header().Set("Zotero-Server-ID", serverID) }

	mux := http.NewServeMux()
	// {$} anchors the match: a bare "GET /api/" would claim the whole subtree and
	// swallow every source read.
	mux.HandleFunc("GET /api/{$}", func(w http.ResponseWriter, _ *http.Request) { setServerID(w) })
	mux.HandleFunc("POST /api/local/authorize", func(w http.ResponseWriter, _ *http.Request) {
		setServerID(w)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "authorized-key", "remember": true})
	})
	mux.HandleFunc("POST /api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		setServerID(w)
		target.handle(w, r)
	})
	if fileURL != nil {
		mux.HandleFunc("GET /api/users/0/items/{key}/file/view/url", fileURL)
	}
	mux.Handle("/", src.server(t).Config.Handler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := zotero.New(srv.URL)
	c.SetWriteAuthorizer(zotero.AllowAllWrites())
	return c, context.Background()
}

// The point of the command is that it does not report success over a tree with
// losses in it. An item whose note failed must not read as "created" — that is
// precisely the silent-loss shape #113 was filed about, moved one level down.
func TestExecuteCopyReportsPartialWhenAChildFails(t *testing.T) {
	src := fullTree()
	target := &copyTarget{rejectType: "note"}
	c, ctx := writableCopyClient(t, src, target, nil)

	planned, err := planItemCopy(ctx, c, zotero.UserLibrary(), "SRC00001", true)
	if err != nil {
		t.Fatalf("planItemCopy: %v", err)
	}
	record := executeItemCopy(ctx, c, zotero.UserLibrary(), zotero.UserLibrary(), 0, planned, nil)

	if record.Key == "" {
		t.Fatal("the item itself should still have been created")
	}
	if record.Status != output.StatusPartial {
		t.Errorf("item status = %q, want %q — a child failed", record.Status, output.StatusPartial)
	}

	var sawFailedNote bool
	for _, child := range record.Children {
		if child.Kind == "note" {
			sawFailedNote = child.Status == output.StatusFailed
			if child.Failure == nil {
				t.Error("the failed note carries no failure to explain it")
			}
		}
	}
	if !sawFailedNote {
		t.Error("the note was not reported as failed")
	}
}

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestAddTags(t *testing.T) {
	existing := []zotero.Tag{{Tag: "a"}, {Tag: "b", Type: 1}}
	next, added := addTags(existing, []string{"a", "c"})
	if len(next) != 3 || next[2].Tag != "c" {
		t.Errorf("next = %+v, want a,b,c", next)
	}
	if strings.Join(added, ",") != "c" {
		t.Errorf("added = %v, want [c] (a already present)", added)
	}
	// automatic tag's type is preserved through the splice
	if next[1].Type != 1 {
		t.Errorf("existing automatic tag lost its type: %+v", next[1])
	}
}

func TestRemoveTags(t *testing.T) {
	existing := []zotero.Tag{{Tag: "a"}, {Tag: "b"}, {Tag: "c"}}
	next, removed := removeTags(existing, []string{"b", "z"})
	if len(next) != 2 || next[0].Tag != "a" || next[1].Tag != "c" {
		t.Errorf("next = %+v, want a,c", next)
	}
	if strings.Join(removed, ",") != "b" {
		t.Errorf("removed = %v, want [b] (z absent)", removed)
	}
}

func TestTagsPatch(t *testing.T) {
	empty, err := tagsPatch(nil)
	if err != nil || string(empty) != `{"tags":[]}` {
		t.Errorf("empty patch = %s (%v), want {\"tags\":[]}", empty, err)
	}
	one, err := tagsPatch([]zotero.Tag{{Tag: "x"}})
	if err != nil || !strings.Contains(string(one), `"tag":"x"`) {
		t.Errorf("patch = %s", one)
	}
}

func TestCapitalize(t *testing.T) {
	if capitalize("add") != "Add" || capitalize("remove") != "Remove" || capitalize("") != "" {
		t.Error("capitalize failed")
	}
}

type tagWriteFake struct {
	itemTags     string // JSON array for the item's existing tags
	patches      atomic.Int32
	deletes      atomic.Int32
	deleteStatus int
}

func newTagWriteFake(t *testing.T, fake *tagWriteFake) *httptest.Server {
	t.Helper()
	if fake.itemTags == "" {
		fake.itemTags = `[{"tag":"keep"}]`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Zotero-Server-ID", "tag-write-test")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items":
			w.Header().Set("Last-Modified-Version", "30")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/users/0/items/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/users/0/items/")
			if strings.HasPrefix(key, "MISS") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"key":"` + key + `","version":777,"data":{"key":"` + key + `","version":777,"itemType":"journalArticle","title":"Tagged","tags":` + fake.itemTags + `}}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/users/0/items/"):
			fake.patches.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/users/0/tags":
			fake.deletes.Add(1)
			status := fake.deleteStatus
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
		default:
			http.NotFound(w, r)
		}
	}))
}

func decodeTagMutationLines(t *testing.T, raw string) []output.TagMutation {
	t.Helper()
	records := make([]output.TagMutation, 0)
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var doc struct {
			Kind output.Kind        `json:"kind"`
			Data output.TagMutation `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("invalid jsonl line %q: %v", line, err)
		}
		if doc.Kind != output.KindTagMutation {
			t.Fatalf("line kind = %q", doc.Kind)
		}
		records = append(records, doc.Data)
	}
	return records
}

func decodeTagMutationDocument(t *testing.T, raw string) []output.TagMutation {
	t.Helper()
	var doc struct {
		Kind output.Kind          `json:"kind"`
		Data []output.TagMutation `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("invalid machine output: %v\n%s", err, raw)
	}
	if doc.Kind != output.KindTagMutations {
		t.Fatalf("document kind = %q", doc.Kind)
	}
	return doc.Data
}

func TestTagAddMachine(t *testing.T) {
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	// Dry run: "keep" already present is unchanged, "new" is planned.
	got, _, err := runCLI(srv.URL, "--json", "tag", "add", "keep", "new", "--item", "ITEM0001", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeTagMutationDocument(t, got)
	if len(recs) != 2 {
		t.Fatalf("records = %+v", recs)
	}
	if recs[0].Tag != "keep" || recs[0].Status != "unchanged" || recs[0].Operation != "add" || recs[0].Item != "ITEM0001" {
		t.Errorf("record 0 = %+v", recs[0])
	}
	if recs[1].Tag != "new" || recs[1].Status != "planned" || recs[1].Index != 1 {
		t.Errorf("record 1 = %+v", recs[1])
	}
	if fake.patches.Load() != 0 {
		t.Fatal("dry run performed a write")
	}

	// Real write: "new" becomes added, one patch.
	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "tag", "add", "keep", "new", "--item", "ITEM0001", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	recs = decodeTagMutationDocument(t, got)
	if recs[0].Status != "unchanged" || recs[1].Status != "added" || fake.patches.Load() != 1 {
		t.Fatalf("records = %+v, patches = %d", recs, fake.patches.Load())
	}
}

func TestTagRemoveMachineUnchangedWhenAbsent(t *testing.T) {
	storeItemWriteKey(t)
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--jsonl", "tag", "remove", "keep", "absent", "--item", "ITEM0001", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeTagMutationLines(t, got)
	if len(recs) != 2 {
		t.Fatalf("records = %+v", recs)
	}
	if recs[0].Tag != "keep" || recs[0].Status != "removed" || recs[0].Operation != "remove" {
		t.Errorf("record 0 = %+v", recs[0])
	}
	if recs[1].Tag != "absent" || recs[1].Status != "unchanged" {
		t.Errorf("record 1 = %+v", recs[1])
	}
	if fake.patches.Load() != 1 {
		t.Fatalf("patches = %d", fake.patches.Load())
	}
}

func TestTagAddMachineNoChangeEmitsUnchanged(t *testing.T) {
	storeItemWriteKey(t)
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	// "keep" is already present, so nothing changes — but machine mode still
	// emits a structured record rather than nothing.
	got, _, err := runCLI(srv.URL, "--json", "tag", "add", "keep", "--item", "ITEM0001", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeTagMutationDocument(t, got)
	if len(recs) != 1 || recs[0].Status != "unchanged" {
		t.Fatalf("records = %+v", recs)
	}
	if fake.patches.Load() != 0 {
		t.Fatalf("no-change wrote to the item: patches = %d", fake.patches.Load())
	}
}

func TestTagAddMachineDeduplicatesRepeatedName(t *testing.T) {
	storeItemWriteKey(t)
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	// "new" is absent and requested twice: the item is added to once, so the
	// first record is "added" and the duplicate reads "unchanged".
	got, _, err := runCLI(srv.URL, "--json", "tag", "add", "new", "new", "--item", "ITEM0001", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeTagMutationDocument(t, got)
	if len(recs) != 2 || recs[0].Status != "added" || recs[1].Status != "unchanged" {
		t.Fatalf("records = %+v", recs)
	}
	if fake.patches.Load() != 1 {
		t.Fatalf("patches = %d, want a single splice", fake.patches.Load())
	}
}

func TestTagDeleteHumanDryRunOutputUnchanged(t *testing.T) {
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "tag", "delete", "urgent", "todo", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	want := "Target: My Library — " + srv.URL + "\nWill remove these tags from EVERY item:\n  - urgent\n  - todo\n\nDry run — nothing was deleted.\n"
	if got != want {
		t.Fatalf("human output changed:\ngot  %q\nwant %q", got, want)
	}
}

func TestTagDeleteMachine(t *testing.T) {
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "tag", "delete", "t1", "t2", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeTagMutationDocument(t, got)
	if len(recs) != 2 || recs[0].Operation != "delete" || recs[0].Status != "planned" || recs[0].Item != "" {
		t.Fatalf("dry-run records = %+v", recs)
	}
	if fake.deletes.Load() != 0 {
		t.Fatal("dry run performed a delete")
	}

	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "tag", "delete", "t1", "t2", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	recs = decodeTagMutationDocument(t, got)
	if len(recs) != 2 || recs[0].Status != "deleted" || recs[1].Status != "deleted" || fake.deletes.Load() != 1 {
		t.Fatalf("records = %+v, deletes = %d", recs, fake.deletes.Load())
	}
}

func TestTagMachineRawRejectedAndYesRequiredEarly(t *testing.T) {
	for _, command := range []string{"add", "remove", "delete"} {
		t.Run("raw_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--raw", "tag", command)
			if !errors.Is(err, output.ErrRawUnavailable) {
				t.Fatalf("err = %v, want ErrRawUnavailable", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
		t.Run("yes_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--json", "tag", command)
			if err == nil || !strings.Contains(err.Error(), "--yes") {
				t.Fatalf("err = %v, want --yes requirement", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

func TestTagAddHumanNoChangeOutputUnchanged(t *testing.T) {
	fake := &tagWriteFake{}
	srv := newTagWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "tag", "add", "keep", "--item", "ITEM0001")
	if err != nil {
		t.Fatal(err)
	}
	want := "Target: My Library — " + srv.URL + "\nITEM0001 (journalArticle): Tagged\nNo change — nothing to add.\n"
	if got != want {
		t.Fatalf("human output changed:\ngot  %q\nwant %q", got, want)
	}
}

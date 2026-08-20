package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

type collectionWriteFake struct {
	createBody   string
	patchStatus  int
	deleteStatus int
	creates      atomic.Int32
	patches      atomic.Int32
	deletes      atomic.Int32
}

func newCollectionWriteFake(t *testing.T, fake *collectionWriteFake) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Zotero-Server-ID", "collection-write-test")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items":
			w.Header().Set("Last-Modified-Version", "20")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/users/0/collections/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/users/0/collections/")
			if strings.HasPrefix(key, "MISS") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"key":"` + key + `","version":777,"data":{"key":"` + key + `","version":777,"name":"Name ` + key + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/users/0/collections":
			fake.creates.Add(1)
			_, _ = w.Write([]byte(fake.createBody))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/users/0/collections/"):
			fake.patches.Add(1)
			status := fake.patchStatus
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/users/0/collections":
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

func decodeCollectionMutationDocument(t *testing.T, raw string) struct {
	Schema int                         `json:"schema"`
	Kind   output.Kind                 `json:"kind"`
	Data   []output.CollectionMutation `json:"data"`
	Meta   json.RawMessage             `json:"meta"`
} {
	t.Helper()
	var doc struct {
		Schema int                         `json:"schema"`
		Kind   output.Kind                 `json:"kind"`
		Data   []output.CollectionMutation `json:"data"`
		Meta   json.RawMessage             `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("invalid machine output: %v\n%s", err, raw)
	}
	return doc
}

func TestCollectionCreateMachineDryRunJSONAndJSONL(t *testing.T) {
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "collection", "create", "Smart Grid", "-p", "PARENT01", "--dry-run")
	if err != nil {
		t.Fatalf("json dry run: %v", err)
	}
	doc := decodeCollectionMutationDocument(t, got)
	if doc.Schema != output.SchemaVersion || doc.Kind != output.KindCollectionMutations || len(doc.Data) != 1 {
		t.Fatalf("document = %+v", doc)
	}
	rec := doc.Data[0]
	if rec.Index != 0 || rec.Operation != "create" || rec.Status != "planned" || rec.Name != "Smart Grid" || rec.ParentKey != "PARENT01" {
		t.Fatalf("record = %+v", rec)
	}
	if len(doc.Meta) != 0 {
		t.Fatalf("mutation document carried meta: %s", got)
	}
	if fake.creates.Load() != 0 {
		t.Fatal("dry run performed a write")
	}

	got, _, err = runCLI(srv.URL, "--jsonl", "collection", "create", "Top Level", "--dry-run")
	if err != nil {
		t.Fatalf("jsonl dry run: %v", err)
	}
	var line struct {
		Kind output.Kind               `json:"kind"`
		Data output.CollectionMutation `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &line); err != nil {
		t.Fatalf("jsonl: %v", err)
	}
	if line.Kind != output.KindCollectionMutation || line.Data.Status != "planned" || line.Data.ParentKey != "" {
		t.Fatalf("jsonl record = %+v", line)
	}
}

func TestCollectionCreateMachineFailureExitsAfterEmitting(t *testing.T) {
	storeItemWriteKey(t)
	fake := &collectionWriteFake{createBody: `{
		"successful":{},
		"unchanged":{},
		"failed":{"0":{"key":"BADCOLL0","code":400,"message":"bad collection"}}
	}`}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "collection", "create", "Doomed", "--yes")
	var exit cli.ExitCoder
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	doc := decodeCollectionMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "failed" || doc.Data[0].Failure == nil || doc.Data[0].Failure.Code != 400 {
		t.Fatalf("record = %+v", doc.Data)
	}
	if strings.Contains(got, "Target:") || strings.Contains(got, `"version"`) {
		t.Fatalf("machine output leaked prose or version:\n%s", got)
	}
	if fake.creates.Load() != 1 {
		t.Fatalf("creates = %d", fake.creates.Load())
	}
}

func TestCollectionCreateResultsRejectsInvalidOutcomes(t *testing.T) {
	cols := []json.RawMessage{
		json.RawMessage(`{"name":"Zero","parentCollection":false}`),
		json.RawMessage(`{"name":"One","parentCollection":"PARENT01"}`),
	}
	res := zotero.WriteResult{
		Successful: map[string]zotero.Envelope{
			"0": {Key: "MADE0"},
			"x": {Key: "MALFORMED"},
		},
		Unchanged: map[string]string{"0": "SAME0"},
		Failed: map[string]zotero.WriteFailure{
			"3": {Key: "OUTSIDE3", Code: 400, Message: "outside request"},
		},
	}

	records, err := collectionCreateResults(cols, res)
	if err == nil {
		t.Fatal("expected invalid write response error")
	}
	for _, text := range []string{`successful index "x"`, `failed index "3"`, "request index 0 has 2 outcomes", "request index 1 has 0 outcomes"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("error %q does not contain %q", err, text)
		}
	}
	if len(records) != len(cols) {
		t.Fatalf("records = %d, want %d", len(records), len(cols))
	}
	// The index-1 record kept its request context even though it had no outcome.
	if records[1].Name != "One" || records[1].ParentKey != "PARENT01" || records[1].Status != "failed" {
		t.Fatalf("index-1 record lost context: %+v", records[1])
	}
}

func TestCollectionRenameMachine(t *testing.T) {
	seedWriteLease(t)
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "collection", "rename", "COLL0001", "New Name", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeCollectionMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Operation != "rename" || doc.Data[0].Status != "planned" || doc.Data[0].Name != "New Name" || doc.Data[0].Key != "COLL0001" {
		t.Fatalf("dry-run record = %+v", doc.Data)
	}

	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "collection", "rename", "COLL0001", "New Name", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeCollectionMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "renamed" || fake.patches.Load() != 1 {
		t.Fatalf("record = %+v, patches = %d", doc.Data, fake.patches.Load())
	}
}

func TestCollectionMoveMachine(t *testing.T) {
	seedWriteLease(t)
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	// Dry run under a new parent: planned, parentKey set, no write.
	got, _, err := runCLI(srv.URL, "--json", "collection", "move", "COLL0001", "--to", "PARENT99", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeCollectionMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Operation != "move" || doc.Data[0].Status != "planned" || doc.Data[0].ParentKey != "PARENT99" {
		t.Fatalf("dry-run record = %+v", doc.Data)
	}
	if fake.patches.Load() != 0 {
		t.Fatal("dry run performed a write")
	}

	// Real move to top level: one patch, status moved, no parentKey.
	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "collection", "move", "COLL0001", "--to-top", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeCollectionMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "moved" || doc.Data[0].ParentKey != "" || fake.patches.Load() != 1 {
		t.Fatalf("record = %+v, patches = %d", doc.Data, fake.patches.Load())
	}
}

func TestCollectionMoveRejectsBadDestination(t *testing.T) {
	// No server should be reached: destination is validated before any request.
	for name, args := range map[string][]string{
		"neither": {"collection", "move", "COLL0001", "--yes"},
		"both":    {"collection", "move", "COLL0001", "--to", "P1", "--to-top", "--yes"},
	} {
		_, _, err := runCLI("http://must-not-be-used.invalid", args...)
		if err == nil {
			t.Errorf("%s: expected a destination error", name)
		}
	}
}

func TestCollectionDeleteMachineNotFoundOrdering(t *testing.T) {
	seedWriteLease(t)
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "collection", "delete", "MISS1", "COLL0001", "MISS2", "COLL0002", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeCollectionMutationDocument(t, got)
	wantStatuses := []string{"notFound", "planned", "notFound", "planned"}
	if len(doc.Data) != len(wantStatuses) {
		t.Fatalf("data = %+v", doc.Data)
	}
	for i, status := range wantStatuses {
		if doc.Data[i].Index != i || doc.Data[i].Status != status || doc.Data[i].Operation != "delete" {
			t.Errorf("record %d = %+v", i, doc.Data[i])
		}
	}
	if fake.deletes.Load() != 0 {
		t.Fatal("dry run performed a delete")
	}

	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--jsonl", "collection", "delete", "COLL0001", "MISS1", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("delete JSONL lines = %d: %s", len(lines), got)
	}
	for i, wantStatus := range []string{"deleted", "notFound"} {
		var line struct {
			Kind output.Kind               `json:"kind"`
			Data output.CollectionMutation `json:"data"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &line); err != nil {
			t.Fatal(err)
		}
		if line.Kind != output.KindCollectionMutation || line.Data.Index != i || line.Data.Status != wantStatus {
			t.Errorf("line %d = %+v", i, line)
		}
	}
	if fake.deletes.Load() != 1 {
		t.Fatalf("deletes = %d", fake.deletes.Load())
	}
}

func TestCollectionMachineRawRejectedAndYesRequiredEarly(t *testing.T) {
	for _, command := range []string{"create", "rename", "delete"} {
		t.Run("raw_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--raw", "collection", command)
			if !errors.Is(err, output.ErrRawUnavailable) {
				t.Fatalf("err = %v, want ErrRawUnavailable", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
		t.Run("yes_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--json", "collection", command)
			if err == nil || !strings.Contains(err.Error(), "--yes") {
				t.Fatalf("err = %v, want --yes requirement", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

func TestCollectionDeleteHumanDryRunOutputUnchanged(t *testing.T) {
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "collection", "delete", "COLL0001", "MISS1", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	want := "Target: My Library — " + srv.URL + "\nWill delete (their items are kept):\n  - COLL0001: Name COLL0001\n  ! MISS1 — not found, skipping\n\nDry run — nothing was deleted.\n"
	if got != want {
		t.Fatalf("human output changed:\ngot  %q\nwant %q", got, want)
	}
}

func TestCollectionCreateHumanDryRunOutputUnchanged(t *testing.T) {
	fake := &collectionWriteFake{}
	srv := newCollectionWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "collection", "create", "Smart Grid", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	want := "Target: My Library — " + srv.URL + "\n  + collection: Smart Grid\n\nDry run — nothing was written.\n"
	if got != want {
		t.Fatalf("human output changed:\ngot  %q\nwant %q", got, want)
	}
}

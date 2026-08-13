package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestParseItemsInput(t *testing.T) {
	single, err := parseItemsInput([]byte(`{"itemType":"book","title":"One"}`))
	if err != nil || len(single) != 1 {
		t.Fatalf("single object → %d items, err %v", len(single), err)
	}
	arr, err := parseItemsInput([]byte(`[{"itemType":"book"},{"itemType":"note"}]`))
	if err != nil || len(arr) != 2 {
		t.Fatalf("array → %d items, err %v", len(arr), err)
	}
	if _, err := parseItemsInput([]byte("   ")); err == nil {
		t.Error("empty input should error")
	}
	if _, err := parseItemsInput([]byte("{bad json")); err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestConfirm(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "Y\n": true, "n\n": false, "\n": false, "nope\n": false}
	for in, want := range cases {
		if got := confirm(strings.NewReader(in), io.Discard, "ok?"); got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSortedIndices(t *testing.T) {
	m := map[string]string{"2": "b", "0": "a", "10": "c", "1": "x"}
	got := sortedIndices(m)
	want := []string{"0", "1", "2", "10"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sortedIndices = %v, want %v", got, want)
	}
}

func TestKeystore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZOTGO_CONFIG_DIR", dir)

	if k := loadLocalKey(); k != "" {
		t.Fatalf("fresh config has a key: %q", k)
	}
	if err := saveLocalKey("SECRET123"); err != nil {
		t.Fatalf("saveLocalKey: %v", err)
	}
	if k := loadLocalKey(); k != "SECRET123" {
		t.Errorf("loadLocalKey = %q, want SECRET123", k)
	}
	// Stored owner-only (Unix permission model only).
	path := filepath.Join(dir, "local-api-key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("key file mode = %o, want 600", perm)
		}
	}
	if err := clearLocalKey(); err != nil {
		t.Fatalf("clearLocalKey: %v", err)
	}
	if k := loadLocalKey(); k != "" {
		t.Errorf("key survived clear: %q", k)
	}
	// Clearing again is not an error.
	if err := clearLocalKey(); err != nil {
		t.Errorf("clear on missing file errored: %v", err)
	}
}

func TestReportWriteResult_OrdersByIndex(t *testing.T) {
	res := zotero.WriteResult{
		Successful: map[string]zotero.Envelope{
			"0": mustEnvelope(`{"key":"AAA"}`),
			"2": mustEnvelope(`{"key":"CCC"}`),
		},
		Failed: map[string]zotero.WriteFailure{
			"1": {Key: "BBB", Code: 400, Message: "bad"},
		},
	}
	var b strings.Builder
	reportWriteResult(&b, res)
	out := b.String()
	if !strings.Contains(out, "created AAA") || !strings.Contains(out, "created CCC") || !strings.Contains(out, "failed [1] BBB") {
		t.Errorf("report missing lines:\n%s", out)
	}
}

func mustEnvelope(s string) zotero.Envelope {
	var e zotero.Envelope
	_ = json.Unmarshal([]byte(s), &e)
	return e
}

func TestParsePatchInput(t *testing.T) {
	if _, err := parsePatchInput([]byte(`{"title":"New"}`)); err != nil {
		t.Errorf("valid object → %v", err)
	}
	for name, in := range map[string]string{
		"array":   `[{"title":"x"}]`,
		"empty":   `   `,
		"emptyOb": `{}`,
		"bad":     `{nope`,
	} {
		if _, err := parsePatchInput([]byte(in)); err == nil {
			t.Errorf("%s should error", name)
		}
	}
}

func TestPatchFields_Sorted(t *testing.T) {
	got := patchFields(json.RawMessage(`{"title":"a","abstractNote":"b","date":"c"}`))
	if strings.Join(got, ",") != "abstractNote,date,title" {
		t.Errorf("patchFields = %v, want sorted", got)
	}
}

type itemWriteFake struct {
	createBody   string
	patchStatus  int
	putStatus    int
	deleteStatus int
	writes       atomic.Int32
	patches      atomic.Int32
	puts         atomic.Int32
	deletes      atomic.Int32
}

func newItemWriteFake(t *testing.T, fake *itemWriteFake) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Zotero-Server-ID", "item-write-test")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items":
			w.Header().Set("Last-Modified-Version", "12")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/users/0/items/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/users/0/items/")
			if strings.HasPrefix(key, "MISS") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"key":"` + key + `","version":777,"data":{"key":"` + key + `","version":777,"itemType":"journalArticle","title":"Title ` + key + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/users/0/items":
			fake.writes.Add(1)
			_, _ = w.Write([]byte(fake.createBody))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/users/0/items/"):
			fake.patches.Add(1)
			status := fake.patchStatus
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/users/0/items/"):
			fake.puts.Add(1)
			status := fake.putStatus
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/users/0/items":
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

func itemInputFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "item.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func storeItemWriteKey(t *testing.T) {
	t.Helper()
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	if err := saveLocalKey("TEST-KEY"); err != nil {
		t.Fatalf("saveLocalKey: %v", err)
	}
}

func decodeItemMutationDocument(t *testing.T, raw string) struct {
	Schema int                   `json:"schema"`
	Kind   output.Kind           `json:"kind"`
	Data   []output.ItemMutation `json:"data"`
	Meta   json.RawMessage       `json:"meta"`
} {
	t.Helper()
	var doc struct {
		Schema int                   `json:"schema"`
		Kind   output.Kind           `json:"kind"`
		Data   []output.ItemMutation `json:"data"`
		Meta   json.RawMessage       `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("invalid machine output: %v\n%s", err, raw)
	}
	return doc
}

func TestItemCreateMachineDryRunJSONAndJSONL(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	file := itemInputFile(t, `[
		{"itemType":"book","title":"First","creators":[{"name":"Do not expose"}],"version":99},
		{"itemType":"note","title":"Second"}
	]`)

	got, _, err := runCLI(srv.URL, "--json", "item", "create", "--dry-run", "--file", file)
	if err != nil {
		t.Fatalf("json dry run: %v", err)
	}
	doc := decodeItemMutationDocument(t, got)
	if doc.Schema != output.SchemaVersion || doc.Kind != output.KindItemMutations || len(doc.Data) != 2 {
		t.Fatalf("document = %+v", doc)
	}
	for i, record := range doc.Data {
		if record.Index != i || record.Operation != "create" || record.Status != "planned" {
			t.Errorf("record %d = %+v", i, record)
		}
	}
	if len(doc.Meta) != 0 || strings.Contains(got, "Do not expose") || strings.Contains(got, `"version"`) {
		t.Fatalf("machine output leaked meta or raw input:\n%s", got)
	}

	got, _, err = runCLI(srv.URL, "--jsonl", "item", "create", "--dry-run", "--file", file)
	if err != nil {
		t.Fatalf("jsonl dry run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d JSONL lines:\n%s", len(lines), got)
	}
	for i, line := range lines {
		var lineDoc struct {
			Kind output.Kind         `json:"kind"`
			Data output.ItemMutation `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &lineDoc); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if lineDoc.Kind != output.KindItemMutation || lineDoc.Data.Index != i {
			t.Errorf("line %d = %+v", i, lineDoc)
		}
	}
	if fake.writes.Load() != 0 {
		t.Fatal("dry run performed a write")
	}
}

func TestItemMachineRawRejectedAndYesRequiredEarly(t *testing.T) {
	for _, command := range []string{"create", "patch", "delete"} {
		t.Run("raw_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--raw", "item", command)
			if !errors.Is(err, output.ErrRawUnavailable) {
				t.Fatalf("err = %v, want ErrRawUnavailable", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
		t.Run("yes_"+command, func(t *testing.T) {
			stdout, _, err := runCLI("http://must-not-be-used.invalid", "--json", "item", command)
			if err == nil || !strings.Contains(err.Error(), "--yes") {
				t.Fatalf("err = %v, want --yes requirement", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}

func TestItemCreateMachinePartialResultOrderedBeforeExit(t *testing.T) {
	storeItemWriteKey(t)
	fake := &itemWriteFake{createBody: `{
		"successful":{"2":{"key":"CREATED2","version":55,"data":{"itemType":"book","title":"Server title","version":55}}},
		"unchanged":{"0":"SAME0000"},
		"failed":{"1":{"key":"BAD00001","code":400,"message":"bad item"}}
	}`}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	file := itemInputFile(t, `[
		{"itemType":"book","title":"Zero"},
		{"itemType":"note","title":"Fallback title"},
		{"itemType":"book","title":"Two"}
	]`)

	got, _, err := runCLI(srv.URL, "--json", "item", "create", "--yes", "--file", file)
	var exit cli.ExitCoder
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	doc := decodeItemMutationDocument(t, got)
	if len(doc.Data) != 3 {
		t.Fatalf("data = %+v", doc.Data)
	}
	wantStatuses := []string{"unchanged", "failed", "created"}
	for i, record := range doc.Data {
		if record.Index != i || record.Status != wantStatuses[i] {
			t.Errorf("record %d = %+v", i, record)
		}
	}
	if doc.Data[1].Title != "Fallback title" || doc.Data[1].Failure == nil || doc.Data[1].Failure.Code != 400 {
		t.Errorf("failed record lost context: %+v", doc.Data[1])
	}
	if strings.Contains(got, `"version"`) || strings.Contains(got, "Target:") {
		t.Fatalf("machine output leaked version or prose:\n%s", got)
	}
	if fake.writes.Load() != 1 {
		t.Fatalf("writes = %d", fake.writes.Load())
	}
}

func TestItemCreateResultsRejectsInvalidOutcomes(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"itemType":"book","title":"Zero"}`),
		json.RawMessage(`{"itemType":"book","title":"One"}`),
		json.RawMessage(`{"itemType":"book","title":"Two"}`),
	}
	res := zotero.WriteResult{
		Successful: map[string]zotero.Envelope{
			"0": {Key: "CREATED0"},
			"x": {Key: "MALFORMED"},
		},
		Unchanged: map[string]string{"0": "SAME0000"},
		Failed: map[string]zotero.WriteFailure{
			"2": {Key: "BAD00002", Code: 400, Message: "bad item"},
			"4": {Key: "OUTSIDE4", Code: 400, Message: "outside request"},
		},
	}

	records, err := itemCreateResults(items, res)
	if err == nil {
		t.Fatal("expected invalid write response error")
	}
	for _, text := range []string{`successful index "x"`, `failed index "4"`, "request index 0 has 2 outcomes", "request index 1 has 0 outcomes"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("error %q does not contain %q", err, text)
		}
	}
	if len(records) != len(items) {
		t.Fatalf("records = %d, want %d", len(records), len(items))
	}
	if records[0].Status != "failed" || records[0].Failure == nil || records[1].Status != "failed" || records[1].Failure == nil {
		t.Fatalf("missing/duplicate outcomes not represented: %+v", records)
	}
	if records[2].Status != "failed" || records[2].Failure == nil || records[2].Failure.Code != 400 {
		t.Fatalf("valid failed outcome lost: %+v", records[2])
	}
	encoded, err := json.Marshal(records[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"code":0`) {
		t.Fatalf("local protocol failure emitted a fake code: %s", encoded)
	}
}

func TestItemPatchMachineArrayAndSortedFields(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	file := itemInputFile(t, `{"title":"New","abstractNote":"A","date":"2020"}`)

	got, _, err := runCLI(srv.URL, "--json", "item", "patch", "AAAA1111", "--dry-run", "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeItemMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "planned" || strings.Join(doc.Data[0].Fields, ",") != "abstractNote,date,title" {
		t.Fatalf("data = %+v", doc.Data)
	}
	if doc.Data[0].Type != "journalArticle" || doc.Data[0].Title != "Title AAAA1111" || strings.Contains(got, `"version"`) {
		t.Fatalf("context/version = %+v\n%s", doc.Data[0], got)
	}

	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "item", "patch", "AAAA1111", "--yes", "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeItemMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "patched" || fake.patches.Load() != 1 {
		t.Fatalf("data = %+v, patches = %d", doc.Data, fake.patches.Load())
	}
}

func TestParseReplaceInput(t *testing.T) {
	if _, err := parseReplaceInput([]byte(`{"itemType":"book","title":"Whole"}`)); err != nil {
		t.Errorf("valid full object → %v", err)
	}
	for name, in := range map[string]string{
		"empty":      `   `,
		"array":      `[{"itemType":"book"}]`,
		"bad":        `{nope`,
		"noItemType": `{"title":"missing type"}`,
	} {
		if _, err := parseReplaceInput([]byte(in)); err == nil {
			t.Errorf("%s should error", name)
		}
	}
}

func TestItemReplaceMachine(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	file := itemInputFile(t, `{"itemType":"book","title":"Replaced Title"}`)

	// Dry run: planned, no write, context from the new object.
	got, _, err := runCLI(srv.URL, "--json", "item", "replace", "AAAA1111", "--dry-run", "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeItemMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Operation != "replace" || doc.Data[0].Status != "planned" {
		t.Fatalf("dry-run record = %+v", doc.Data)
	}
	if doc.Data[0].Type != "book" || doc.Data[0].Title != "Replaced Title" {
		t.Fatalf("context not taken from new object: %+v", doc.Data[0])
	}
	if fake.puts.Load() != 0 {
		t.Fatal("dry run performed a PUT")
	}

	// Real replace: one PUT, status replaced.
	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "item", "replace", "AAAA1111", "--yes", "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeItemMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "replaced" || fake.puts.Load() != 1 {
		t.Fatalf("record = %+v, puts = %d", doc.Data, fake.puts.Load())
	}
}

func TestItemDeleteMachineNotFoundOrderingAndTransportFailure(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()

	got, _, err := runCLI(srv.URL, "--json", "item", "delete", "MISS1", "AAAA1111", "MISS2", "BBBB2222", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeItemMutationDocument(t, got)
	wantStatuses := []string{"notFound", "planned", "notFound", "planned"}
	if len(doc.Data) != len(wantStatuses) {
		t.Fatalf("data = %+v", doc.Data)
	}
	for i, status := range wantStatuses {
		if doc.Data[i].Index != i || doc.Data[i].Status != status {
			t.Errorf("record %d = %+v", i, doc.Data[i])
		}
	}

	storeItemWriteKey(t)
	got, _, err = runCLI(srv.URL, "--json", "item", "delete", "MISS1", "MISS2", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeItemMutationDocument(t, got)
	if len(doc.Data) != 2 || doc.Data[0].Status != "notFound" || doc.Data[1].Status != "notFound" || fake.deletes.Load() != 0 {
		t.Fatalf("all missing data = %+v, deletes = %d", doc.Data, fake.deletes.Load())
	}

	got, _, err = runCLI(srv.URL, "--jsonl", "item", "delete", "AAAA1111", "MISS1", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("delete JSONL lines = %d: %s", len(lines), got)
	}
	for i, wantStatus := range []string{"deleted", "notFound"} {
		var lineDoc struct {
			Kind output.Kind         `json:"kind"`
			Data output.ItemMutation `json:"data"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &lineDoc); err != nil {
			t.Fatal(err)
		}
		if lineDoc.Kind != output.KindItemMutation || lineDoc.Data.Index != i || lineDoc.Data.Status != wantStatus {
			t.Errorf("line %d = %+v", i, lineDoc)
		}
	}
	if fake.deletes.Load() != 1 {
		t.Fatalf("deletes = %d", fake.deletes.Load())
	}

	fake.deleteStatus = http.StatusInternalServerError
	got, _, err = runCLI(srv.URL, "--json", "item", "delete", "AAAA1111", "--yes")
	if err == nil {
		t.Fatal("expected delete transport/status error")
	}
	if got != "" || strings.Contains(got, "deleted") {
		t.Fatalf("fabricated delete output: %q", got)
	}
}

func TestItemCreateHumanDryRunOutputUnchanged(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	file := itemInputFile(t, `{"itemType":"book","title":"One"}`)

	got, _, err := runCLI(srv.URL, "item", "create", "--dry-run", "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	want := "Target: My Library — " + srv.URL + "\n  + book: One\n\nDry run — nothing was written.\n"
	if got != want {
		t.Fatalf("human output changed:\ngot  %q\nwant %q", got, want)
	}
}

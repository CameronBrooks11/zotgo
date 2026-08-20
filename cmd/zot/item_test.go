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

func TestManagedAttachmentPatchSafety(t *testing.T) {
	managedModes := []string{"imported_file", "imported_url", "embedded_image"}
	for _, mode := range managedModes {
		item := mustEnvelope(`{"key":"ATTACH01","data":{"itemType":"attachment","linkMode":"` + mode + `"}}`)
		for _, field := range attachmentStorageFields {
			t.Run(mode+"_"+field, func(t *testing.T) {
				err := validateItemPatchSafety(item, json.RawMessage(`{"`+field+`":"value"}`))
				if err == nil || !strings.Contains(err.Error(), field) {
					t.Fatalf("error = %v", err)
				}
			})
		}
		if err := validateItemPatchSafety(item, json.RawMessage(`{"title":"New title","contentType":"application/pdf"}`)); err != nil {
			t.Fatalf("metadata patch for %s: %v", mode, err)
		}
	}

	for _, mode := range []string{"linked_file", "linked_url"} {
		item := mustEnvelope(`{"key":"ATTACH02","data":{"itemType":"attachment","linkMode":"` + mode + `"}}`)
		if err := validateItemPatchSafety(item, json.RawMessage(`{"path":"new location"}`)); err != nil {
			t.Fatalf("linked mode %s: %v", mode, err)
		}
		if err := validateItemPatchSafety(item, json.RawMessage(`{"linkMode":"`+mode+`"}`)); err != nil {
			t.Fatalf("unchanged linked mode %s: %v", mode, err)
		}
	}
	for _, test := range []struct {
		name  string
		item  string
		patch string
	}{
		{name: "linked file to imported file", item: `{"itemType":"attachment","linkMode":"linked_file"}`, patch: `{"linkMode":"imported_file"}`},
		{name: "linked URL to embedded image", item: `{"itemType":"attachment","linkMode":"linked_url"}`, patch: `{"linkMode":"embedded_image"}`},
		{name: "between linked modes", item: `{"itemType":"attachment","linkMode":"linked_file"}`, patch: `{"linkMode":"linked_url"}`},
		{name: "item to managed attachment", item: `{"itemType":"journalArticle"}`, patch: `{"itemType":"attachment","linkMode":"imported_file"}`},
		{name: "managed attachment to item", item: `{"itemType":"attachment","linkMode":"imported_file"}`, patch: `{"itemType":"journalArticle"}`},
		{name: "linked attachment to item", item: `{"itemType":"attachment","linkMode":"linked_file"}`, patch: `{"itemType":"journalArticle"}`},
		{name: "item to linked attachment", item: `{"itemType":"journalArticle"}`, patch: `{"itemType":"attachment","linkMode":"linked_file"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := mustEnvelope(`{"key":"ATTACH02","data":` + test.item + `}`)
			if err := validateItemPatchSafety(item, json.RawMessage(test.patch)); err == nil {
				t.Fatal("expected storage-mode transition rejection")
			}
		})
	}
	for _, data := range []string{
		`{"itemType":"attachment","linkMode":"future_stored_mode"}`,
		`{"itemType":"attachment"}`,
		`{"title":"missing item type"}`,
	} {
		item := mustEnvelope(`{"key":"ATTACH03","data":` + data + `}`)
		if err := validateItemPatchSafety(item, json.RawMessage(`{"path":"new location"}`)); err == nil {
			t.Fatalf("unsafe item data accepted: %s", data)
		}
	}
	article := mustEnvelope(`{"key":"ITEM0001","data":{"itemType":"journalArticle","title":"Paper"}}`)
	if err := validateItemPatchSafety(article, json.RawMessage(`{"filename":"ignored.pdf"}`)); err != nil {
		t.Fatalf("bibliographic item: %v", err)
	}
	if err := validateItemPatchSafety(article, json.RawMessage(`{"itemType":"book"}`)); err != nil {
		t.Fatalf("bibliographic item type change: %v", err)
	}
	linked := mustEnvelope(`{"key":"ATTACH04","data":{"itemType":"attachment","linkMode":"linked_file"}}`)
	for _, patch := range []string{`{"itemType":null}`, `{"linkMode":null}`} {
		if err := validateItemPatchSafety(linked, json.RawMessage(patch)); err == nil {
			t.Fatalf("null storage identity accepted: %s", patch)
		}
	}
}

func TestManagedAttachmentReplaceSafety(t *testing.T) {
	for _, mode := range []string{"imported_file", "imported_url", "embedded_image", "future_stored_mode", ""} {
		t.Run("current "+mode, func(t *testing.T) {
			item := mustEnvelope(`{"key":"ATTACH01","data":{"itemType":"attachment","linkMode":"` + mode + `"}}`)
			err := validateItemReplaceSafety(item, json.RawMessage(`{"itemType":"attachment","linkMode":"linked_file"}`))
			if err == nil {
				t.Fatal("expected replacement rejection")
			}
		})
	}
	for _, test := range []struct {
		name        string
		item        string
		replacement string
	}{
		{name: "linked to managed", item: `{"itemType":"attachment","linkMode":"linked_file"}`, replacement: `{"itemType":"attachment","linkMode":"imported_file"}`},
		{name: "item to managed", item: `{"itemType":"journalArticle"}`, replacement: `{"itemType":"attachment","linkMode":"imported_url"}`},
		{name: "between linked modes", item: `{"itemType":"attachment","linkMode":"linked_file"}`, replacement: `{"itemType":"attachment","linkMode":"linked_url"}`},
		{name: "unknown proposed mode", item: `{"itemType":"journalArticle"}`, replacement: `{"itemType":"attachment","linkMode":"future_mode"}`},
		{name: "managed attachment to item", item: `{"itemType":"attachment","linkMode":"imported_file"}`, replacement: `{"itemType":"journalArticle"}`},
		{name: "linked attachment to item", item: `{"itemType":"attachment","linkMode":"linked_file"}`, replacement: `{"itemType":"journalArticle"}`},
		{name: "item to linked attachment", item: `{"itemType":"journalArticle"}`, replacement: `{"itemType":"attachment","linkMode":"linked_file"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := mustEnvelope(`{"key":"ATTACH02","data":` + test.item + `}`)
			if err := validateItemReplaceSafety(item, json.RawMessage(test.replacement)); err == nil {
				t.Fatal("expected replacement rejection")
			}
		})
	}
	for _, test := range []struct {
		name        string
		item        string
		replacement string
	}{
		{name: "linked file", item: `{"itemType":"attachment","linkMode":"linked_file"}`, replacement: `{"itemType":"attachment","linkMode":"linked_file","path":"attachments/new.pdf"}`},
		{name: "linked URL", item: `{"itemType":"attachment","linkMode":"linked_url"}`, replacement: `{"itemType":"attachment","linkMode":"linked_url","url":"https://example.test"}`},
		{name: "bibliographic item", item: `{"itemType":"journalArticle"}`, replacement: `{"itemType":"book","title":"Book"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := mustEnvelope(`{"key":"ITEM0001","data":` + test.item + `}`)
			if err := validateItemReplaceSafety(item, json.RawMessage(test.replacement)); err != nil {
				t.Fatalf("allowed replacement: %v", err)
			}
		})
	}
}

func TestManagedAttachmentUnsafeUpdatesStopAfterRead(t *testing.T) {
	operations := []struct {
		name    string
		command string
		input   string
	}{
		{name: "patch filename", command: "patch", input: `{"filename":"new.pdf"}`},
		{name: "patch path", command: "patch", input: `{"path":"storage:new.pdf"}`},
		{name: "patch link mode", command: "patch", input: `{"linkMode":"linked_file"}`},
		{name: "patch item type", command: "patch", input: `{"itemType":"journalArticle"}`},
		{name: "replace", command: "replace", input: `{"itemType":"attachment","title":"New title"}`},
	}
	modes := []struct {
		name   string
		before []string
		after  []string
	}{
		{name: "human yes", after: []string{"--yes"}},
		{name: "human dry run", after: []string{"--dry-run"}},
		{name: "JSON dry run", before: []string{"--json"}, after: []string{"--dry-run"}},
		{name: "JSONL dry run", before: []string{"--jsonl"}, after: []string{"--dry-run"}},
	}
	for _, op := range operations {
		for _, mode := range modes {
			t.Run(op.name+"_"+mode.name, func(t *testing.T) {
				t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
				var requests []string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method+" "+r.URL.Path)
					if r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items/ATTACH01" {
						_, _ = w.Write([]byte(`{"key":"ATTACH01","version":7,"data":{"itemType":"attachment","linkMode":"imported_file","filename":"old.pdf"}}`))
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
				}))
				defer srv.Close()

				file := itemInputFile(t, op.input)
				args := append([]string{}, mode.before...)
				args = append(args, "item", op.command, "ATTACH01", "--file", file)
				args = append(args, mode.after...)
				stdout, _, err := runCLI(srv.URL, args...)
				if err == nil {
					t.Fatal("expected update rejection")
				}
				if stdout != "" {
					t.Fatalf("stdout = %q", stdout)
				}
				if got := strings.Join(requests, ","); got != "GET /api/users/0/items/ATTACH01" {
					t.Fatalf("requests = %s", got)
				}
			})
		}
	}
}

func TestManagedAttachmentTransitionsStopAfterRead(t *testing.T) {
	for _, test := range []struct {
		command string
		input   string
	}{
		{command: "patch", input: `{"linkMode":"imported_file"}`},
		{command: "replace", input: `{"itemType":"attachment","linkMode":"imported_file","filename":"paper.pdf"}`},
	} {
		t.Run(test.command, func(t *testing.T) {
			t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
			var requests []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				if r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items/ATTACH01" {
					_, _ = w.Write([]byte(`{"key":"ATTACH01","version":7,"data":{"itemType":"attachment","linkMode":"linked_file","path":"attachments/paper.pdf"}}`))
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			file := itemInputFile(t, test.input)
			stdout, _, err := runCLI(srv.URL, "--json", "item", test.command, "ATTACH01", "--file", file, "--dry-run")
			if err == nil {
				t.Fatal("expected managed-mode transition rejection")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q", stdout)
			}
			if got := strings.Join(requests, ","); got != "GET /api/users/0/items/ATTACH01" {
				t.Fatalf("requests = %s", got)
			}
		})
	}
}

func TestAllowedAttachmentPatchesReachWrite(t *testing.T) {
	for _, tt := range []struct {
		name     string
		itemData string
		patch    string
	}{
		{name: "managed metadata", itemData: `{"itemType":"attachment","linkMode":"imported_file","filename":"old.pdf","title":"Old title"}`, patch: `{"title":"New title","contentType":"application/pdf"}`},
		{name: "linked path", itemData: `{"itemType":"attachment","linkMode":"linked_file","path":"attachments/old.pdf"}`, patch: `{"path":"attachments/new.pdf"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			storeItemWriteKey(t)
			var requests []string
			var patchBody string
			var versionHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Zotero-Server-ID", "attachment-patch-test")
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items/ATTACH01":
					_, _ = w.Write([]byte(`{"key":"ATTACH01","version":7,"data":` + tt.itemData + `}`))
				case r.Method == http.MethodPatch && r.URL.Path == "/api/users/0/items/ATTACH01":
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					patchBody = string(body)
					versionHeader = r.Header.Get("If-Unmodified-Since-Version")
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			file := itemInputFile(t, tt.patch)
			stdout, _, err := runCLI(srv.URL, "item", "patch", "ATTACH01", "--file", file, "--yes")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(requests, ","); got != "GET /api/users/0/items/ATTACH01,PATCH /api/users/0/items/ATTACH01" {
				t.Fatalf("requests = %s", got)
			}
			if patchBody != tt.patch || versionHeader != "7" {
				t.Fatalf("body = %s, version = %q", patchBody, versionHeader)
			}
			if !strings.Contains(stdout, "patched ATTACH01") {
				t.Fatalf("stdout = %q", stdout)
			}
		})
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

// readOnlyBuildServer stands in for a Zotero whose Local API is enabled but has
// no write endpoints: it never sends the Zotero-Server-ID header. It records any
// authorize or write attempt so a test can prove the write failed before them.
func readOnlyBuildServer(t *testing.T, authorized, wrote *atomic.Bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/":
			_, _ = w.Write([]byte(`{}`)) // 200, but no Zotero-Server-ID
		case r.URL.Path == "/api/local/authorize":
			authorized.Store(true)
			w.WriteHeader(http.StatusNotFound)
		case r.Method != http.MethodGet:
			wrote.Store(true)
			w.WriteHeader(http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestItemWriteFailsFastWithoutWriteCapability(t *testing.T) {
	var authorized, wrote atomic.Bool
	srv := readOnlyBuildServer(t, &authorized, &wrote)
	defer srv.Close()
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	file := itemInputFile(t, `[{"itemType":"book","title":"X"}]`)

	_, _, err := runCLI(srv.URL, "item", "create", "--file", file, "--yes")
	if err == nil || !strings.Contains(err.Error(), "no local write API") || !strings.Contains(err.Error(), "5015") {
		t.Fatalf("error = %v, want the actionable no-write-API reason", err)
	}
	if authorized.Load() {
		t.Error("authorized on a build with no write API — must fail before the prompt")
	}
	if wrote.Load() {
		t.Error("attempted a write on a build with no write API")
	}
}

func TestItemCreateDryRunWorksWithoutWriteCapability(t *testing.T) {
	var authorized, wrote atomic.Bool
	srv := readOnlyBuildServer(t, &authorized, &wrote)
	defer srv.Close()
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	file := itemInputFile(t, `[{"itemType":"book","title":"X"}]`)

	// If dry-run consulted the write capability it would error on this build;
	// succeeding proves --dry-run stays usable on a read-only Zotero.
	got, _, err := runCLI(srv.URL, "--json", "item", "create", "--dry-run", "--file", file)
	if err != nil {
		t.Fatalf("dry run on a read-only build: %v", err)
	}
	doc := decodeItemMutationDocument(t, got)
	if len(doc.Data) != 1 || doc.Data[0].Status != "planned" {
		t.Fatalf("document = %+v", doc)
	}
	if authorized.Load() || wrote.Load() {
		t.Error("dry run authorized or wrote; it must do neither")
	}
}

func TestIsTerminalOnRegularFileIsFalse(t *testing.T) {
	f, err := os.Open(itemInputFile(t, "{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("a regular file reported as a terminal")
	}
}

// A persisted key must not paper over a read-only build: RequireWriteCapability
// runs before ensureLocalKey's has-key short-circuit, so a stale key still fails
// fast rather than reaching the write and 404ing.
func TestItemWriteWithStaleKeyStillFailsFastWithoutWriteCapability(t *testing.T) {
	var authorized, wrote atomic.Bool
	srv := readOnlyBuildServer(t, &authorized, &wrote)
	defer srv.Close()
	storeItemWriteKey(t) // persists a key under an isolated ZOTGO_CONFIG_DIR
	file := itemInputFile(t, `[{"itemType":"book","title":"X"}]`)

	_, _, err := runCLI(srv.URL, "item", "create", "--file", file, "--yes")
	if err == nil || !strings.Contains(err.Error(), "no local write API") {
		t.Fatalf("error = %v, want the no-write-API reason despite a persisted key", err)
	}
	if wrote.Load() {
		t.Error("wrote on a read-only build even though a key was persisted")
	}
}

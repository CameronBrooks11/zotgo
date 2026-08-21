package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

const testPDF = "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"

var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

type importServerOptions struct {
	content               []byte
	filename              string
	contentType           string
	duplicate             bool
	duplicateSecondPage   bool
	duplicatePair         bool
	wrongDuplicateSize    bool
	authorizationExists   bool
	authorizeStatus       int
	uploadStatus          int
	registerStatus        int
	registerDespiteError  bool
	wrongVerificationSize bool
	failureBody           string
	createUnauthorized    bool
	malformedCreateReply  bool
	createReplyShape      string
	oneTimeAuthorization  bool
}

type importServerState struct {
	writes         int
	authorizations int
	uploads        int
	registrations  int
	attachmentKey  string
	metadata       map[string]any
	uploadForm     url.Values
	uploaded       []byte
	registered     bool
	duplicateReads int
	deletes        int
}

func writeAttachmentTestFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mtime := time.UnixMilli(1700000000000)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func writeAttachmentTestPDF(t *testing.T) string {
	return writeAttachmentTestFile(t, "paper.pdf", []byte(testPDF))
}

func newAttachmentImportServer(t *testing.T, opts importServerOptions) (*httptest.Server, *importServerState) {
	t.Helper()
	if opts.content == nil {
		opts.content = []byte(testPDF)
	}
	if opts.filename == "" {
		opts.filename = "managed.pdf"
	}
	if opts.contentType == "" {
		opts.contentType = "application/pdf"
	}
	state := &importServerState{}
	checksumBytes := md5.Sum(opts.content)
	checksum := hex.EncodeToString(checksumBytes[:])
	const serverID = "SERVERID1234"
	var baseURL string
	mux := http.NewServeMux()
	setServerID := func(w http.ResponseWriter) { w.Header().Set("Zotero-Server-ID", serverID) }
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		setServerID(w)
	})
	mux.HandleFunc("POST /api/local/authorize", func(w http.ResponseWriter, _ *http.Request) {
		state.authorizations++
		setServerID(w)
		remember := !opts.oneTimeAuthorization
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "authorized-key", "remember": remember})
	})
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		setServerID(w)
		key := r.PathValue("key")
		if key == "PARENT01" {
			_, _ = w.Write([]byte(`{"key":"PARENT01","version":"","data":{"key":"PARENT01","itemType":"journalArticle","title":"Paper"}}`))
			return
		}
		if key != state.attachmentKey || key == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data := map[string]any{
			"key": key, "itemType": "attachment", "parentItem": "PARENT01",
			"linkMode": "imported_file", "title": state.metadata["title"],
			"contentType": opts.contentType, "filename": opts.filename,
			"url": state.metadata["url"], "tags": []any{},
		}
		links := map[string]any{}
		if state.registered {
			data["md5"] = state.uploadForm.Get("md5")
			mtimeValue, err := strconv.ParseInt(state.uploadForm.Get("mtime"), 10, 64)
			if err != nil {
				t.Fatalf("parse mtime: %v", err)
			}
			data["mtime"] = mtimeValue
			length := len(state.uploaded)
			if opts.wrongVerificationSize {
				length++
			}
			links["enclosure"] = map[string]any{
				"href": baseURL + "/api/users/0/items/" + key + "/file/view",
				"type": opts.contentType, "title": opts.filename, "length": length,
			}
		} else {
			data["md5"] = nil
			data["mtime"] = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"key": key, "version": "", "links": links, "data": data})
	})
	mux.HandleFunc("GET /api/users/0/items/{key}/children", func(w http.ResponseWriter, r *http.Request) {
		setServerID(w)
		state.duplicateReads++
		if key := r.PathValue("key"); key != "PARENT01" {
			t.Errorf("duplicate scan used the wrong parent %q; must use the children route for the target", key)
		}
		query := r.URL.Query()
		if query.Get("itemType") != "attachment" || query.Get("limit") != "100" {
			t.Errorf("duplicate query = %s", r.URL.RawQuery)
		}
		if !opts.duplicate {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if opts.duplicateSecondPage && query.Get("start") == "" {
			w.Header().Set("Link", `<`+baseURL+`/api/users/0/items/PARENT01/children?itemType=attachment&limit=100&start=100>; rel="next"`)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		duplicateLength := len(opts.content)
		if opts.wrongDuplicateSize {
			duplicateLength++
		}
		duplicateItem := func(key string) map[string]any {
			return map[string]any{
				"key": key, "version": "",
				"links": map[string]any{"enclosure": map[string]any{
					"href": baseURL + "/api/users/0/items/" + key + "/file/view",
					"type": opts.contentType, "title": "existing.pdf", "length": duplicateLength,
				}},
				"data": map[string]any{
					"itemType": "attachment", "parentItem": "PARENT01", "title": "Existing attachment",
					"linkMode": "imported_file", "contentType": opts.contentType, "filename": "existing.pdf",
					"tags": []any{}, "md5": checksum, "mtime": int64(1700000000000),
				},
			}
		}
		duplicates := []any{duplicateItem("DUPL0001")}
		if opts.duplicatePair {
			duplicates = []any{duplicateItem("ZZZZ0002"), duplicateItem("AAAA0001")}
		}
		_ = json.NewEncoder(w).Encode(duplicates)
	})
	mux.HandleFunc("DELETE /api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		state.deletes++
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		state.writes++
		setServerID(w)
		if opts.createUnauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Zotero-API-Key") == "" || r.Header.Get("Zotero-Server-ID") != serverID {
			t.Errorf("create headers = %#v", r.Header)
		}
		var items []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("created items = %d", len(items))
		}
		item := items[0]
		state.metadata = item
		state.attachmentKey = "NEWATT01"
		for _, forbidden := range []string{"key", "version", "filename", "path", "md5", "mtime"} {
			if _, ok := item[forbidden]; ok {
				t.Errorf("metadata create included %q: %#v", forbidden, item)
			}
		}
		if item["parentItem"] != "PARENT01" || item["linkMode"] != "imported_file" || item["contentType"] != opts.contentType {
			t.Errorf("metadata = %#v", item)
		}
		if opts.malformedCreateReply {
			_, _ = w.Write([]byte(`{"successful":`))
			return
		}
		switch opts.createReplyShape {
		case "conflicting":
			_, _ = w.Write([]byte(`{"successful":{"0":{"key":"NEWATT01"}},"unchanged":{},"failed":{"0":{"code":400,"message":"conflict"}}}`))
			return
		case "wrong-failure-index":
			_, _ = w.Write([]byte(`{"successful":{},"unchanged":{},"failed":{"1":{"code":400,"message":"wrong index"}}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"successful": map[string]any{"0": map[string]any{
				"key": state.attachmentKey, "version": 1,
				"links": map[string]any{"enclosure": map[string]any{"href": false}},
				"data":  map[string]any{"key": state.attachmentKey, "itemType": "attachment"},
			}},
			"unchanged": map[string]any{}, "failed": map[string]any{},
		})
	})
	mux.HandleFunc("POST /api/users/0/items/{key}/file", func(w http.ResponseWriter, r *http.Request) {
		setServerID(w)
		if r.PathValue("key") != state.attachmentKey || r.Header.Get("Zotero-API-Key") == "" || r.Header.Get("If-None-Match") != "*" {
			t.Errorf("file request key/headers = %s %#v", r.PathValue("key"), r.Header)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("upload") != "" {
			state.registrations++
			if opts.registerStatus == 0 || opts.registerStatus == http.StatusNoContent || opts.registerDespiteError {
				state.registered = true
			}
			status := opts.registerStatus
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
			if opts.failureBody != "" {
				_, _ = w.Write([]byte(opts.failureBody))
			}
			return
		}
		state.uploadForm = values
		if values.Get("contentType") != opts.contentType {
			t.Errorf("upload contentType = %q, want %q", values.Get("contentType"), opts.contentType)
		}
		if opts.authorizeStatus != 0 {
			w.WriteHeader(opts.authorizeStatus)
			if opts.failureBody != "" {
				_, _ = w.Write([]byte(opts.failureBody))
			}
			return
		}
		if opts.authorizationExists {
			state.registered = true
			state.uploaded = append([]byte(nil), opts.content...)
			_, _ = w.Write([]byte(`{"exists":1}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url": baseURL + "/api/local/uploads/UPLOADKEY", "uploadKey": "UPLOADKEY",
			"contentType": opts.contentType, "prefix": "", "suffix": "",
		})
	})
	mux.HandleFunc("POST /api/local/uploads/UPLOADKEY", func(w http.ResponseWriter, r *http.Request) {
		state.uploads++
		if r.Header.Get("Zotero-API-Key") != "" || r.Header.Get("Zotero-Server-ID") != "" {
			t.Errorf("receiver got credentials: %#v", r.Header)
		}
		state.uploaded, _ = io.ReadAll(r.Body)
		status := opts.uploadStatus
		if status == 0 {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
		if opts.failureBody != "" {
			_, _ = w.Write([]byte(opts.failureBody))
		}
	})
	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	return srv, state
}

func saveImportTestKey(t *testing.T) {
	t.Helper()
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	if err := saveLocalKey("persistent-key"); err != nil {
		t.Fatalf("saveLocalKey: %v", err)
	}
	// A non-interactive import now needs a lease permitting attachment.import.
	seedWriteLease(t)
}

func importArgs(path string) []string {
	return []string{
		"--json", "attachment", "import", "--parent", "PARENT01", "--file", path,
		"--filename", "managed.pdf", "--title", "Full Text PDF",
		"--source-url", "https://example.com/paper.pdf", "--yes",
	}
}

func TestAttachmentImportPNGSuccess(t *testing.T) {
	path := writeAttachmentTestFile(t, "source.png", testPNG)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{
		content: testPNG, filename: "managed.png", contentType: "image/png",
	})
	defer srv.Close()
	args := []string{
		"--json", "attachment", "import", "--parent", "PARENT01", "--file", path,
		"--filename", "managed.png", "--title", "Figure", "--yes",
	}
	out, _, err := runCLI(srv.URL, args...)
	if err != nil {
		t.Fatalf("PNG import: %v\n%s", err, out)
	}
	if state.metadata["contentType"] != "image/png" || state.uploadForm.Get("contentType") != "image/png" || string(state.uploaded) != string(testPNG) {
		t.Fatalf("MIME propagation state = %#v", state)
	}
	if !strings.Contains(out, `"contentType": "image/png"`) || !strings.Contains(out, `"status": "imported"`) {
		t.Fatalf("PNG output = %s", out)
	}
}

func TestAttachmentImportContentTypeOverride(t *testing.T) {
	path := writeAttachmentTestFile(t, "source.png", testPNG)
	saveImportTestKey(t)
	const override = "application/x-zotgo-image"
	srv, state := newAttachmentImportServer(t, importServerOptions{
		content: testPNG, filename: "managed.png", contentType: override,
	})
	defer srv.Close()
	args := []string{
		"--json", "attachment", "import", "--parent", "PARENT01", "--file", path,
		"--filename", "managed.png", "--content-type", override, "--yes",
	}
	out, _, err := runCLI(srv.URL, args...)
	if err != nil {
		t.Fatalf("override import: %v\n%s", err, out)
	}
	if state.metadata["contentType"] != override || state.uploadForm.Get("contentType") != override || !strings.Contains(out, `"contentType": "`+override+`"`) {
		t.Fatalf("override propagation state=%#v output=%s", state, out)
	}
}

func TestAttachmentImportRejectsInvalidContentTypeBeforeIO(t *testing.T) {
	path := writeAttachmentTestFile(t, "source.png", testPNG)
	out, _, err := runCLI("http://127.0.0.1:1",
		"--json", "attachment", "import", "--parent", "PARENT01", "--file", path,
		"--content-type", "not a mime type", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "invalid --content-type") {
		t.Fatalf("error = %v, output = %s", err, out)
	}
}

func TestAttachmentImportJSONSuccess(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("attachment import: %v\n%s", err, out)
	}
	var doc struct {
		Schema int    `json:"schema"`
		Kind   string `json:"kind"`
		Data   struct {
			Status        string  `json:"status"`
			Stage         string  `json:"stage"`
			ParentKey     string  `json:"parentKey"`
			AttachmentKey *string `json:"attachmentKey"`
			Filename      string  `json:"filename"`
			FileStatus    struct {
				State string `json:"state"`
			} `json:"fileStatus"`
			Verification struct {
				Parent, ManagedStorage, Title, SourceURL bool
				Filename, ContentType, Size, Checksum    bool
				ActualFilename                           string `json:"actualFilename"`
			} `json:"verification"`
			Failure any `json:"failure"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if doc.Schema != 2 || doc.Kind != "attachment-import" || doc.Data.Status != "imported" || doc.Data.Stage != "verified" || doc.Data.ParentKey != "PARENT01" || doc.Data.AttachmentKey == nil || *doc.Data.AttachmentKey != state.attachmentKey || doc.Data.Filename != "managed.pdf" {
		t.Fatalf("document = %#v", doc)
	}
	if doc.Data.FileStatus.State != "metadata-available" || doc.Data.Failure != nil {
		t.Fatalf("status/failure = %#v / %#v", doc.Data.FileStatus, doc.Data.Failure)
	}
	v := doc.Data.Verification
	if !v.Parent || !v.ManagedStorage || !v.Title || !v.SourceURL || !v.Filename || !v.ContentType || !v.Size || !v.Checksum || v.ActualFilename != "managed.pdf" {
		t.Fatalf("verification = %#v", v)
	}
	if state.writes != 1 || state.uploads != 1 || state.registrations != 1 || string(state.uploaded) != testPDF {
		t.Fatalf("state = %#v", state)
	}
	if strings.Contains(out, path) || strings.Contains(out, "UPLOADKEY") || strings.Contains(out, "persistent-key") {
		t.Fatalf("output leaked private/protocol data: %s", out)
	}
}

func TestAttachmentImportExistingAuthorizedBytesSkipUploadAndRegistration(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{authorizationExists: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("existing-byte import: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"status": "imported"`) || state.writes != 1 || state.uploads != 0 || state.registrations != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportDryRunHasNoWritesOrAuthorization(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{})
	defer srv.Close()
	args := importArgs(path)
	args = append(args[:len(args)-1], "--dry-run")
	out, _, err := runCLI(srv.URL, args...)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, `"status": "planned"`) || !strings.Contains(out, `"attachmentKey": null`) || !strings.Contains(out, `"fileStatus": null`) {
		t.Fatalf("dry-run output = %s", out)
	}
	if state.writes != 0 || state.authorizations != 0 || state.uploads != 0 || state.registrations != 0 {
		t.Fatalf("dry run mutated: %#v", state)
	}
}

func TestAttachmentImportDuplicateIsNoOp(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{duplicate: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("duplicate import: %v", err)
	}
	if !strings.Contains(out, `"status": "duplicate"`) || !strings.Contains(out, `"attachmentKey": "DUPL0001"`) || !strings.Contains(out, `"filename": "existing.pdf"`) {
		t.Fatalf("duplicate output = %s", out)
	}
	if state.writes != 0 || state.uploads != 0 || state.registrations != 0 {
		t.Fatalf("duplicate mutated: %#v", state)
	}
}

func TestAttachmentImportDuplicateSelectionIsDeterministic(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{duplicate: true, duplicatePair: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("duplicate import: %v", err)
	}
	if !strings.Contains(out, `"attachmentKey": "AAAA0001"`) || state.writes != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportRejectsMatchingMD5WithConflictingSize(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{duplicate: true, wrongDuplicateSize: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err == nil || !strings.Contains(err.Error(), "matching MD5 but conflicting size") {
		t.Fatalf("error=%v output=%s", err, out)
	}
	if out != "" || state.writes != 0 {
		t.Fatalf("output=%q state=%#v", out, state)
	}
}

func TestAttachmentImportDuplicateScanFollowsPagination(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{duplicate: true, duplicateSecondPage: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("duplicate import: %v", err)
	}
	if !strings.Contains(out, `"status": "duplicate"`) || state.duplicateReads != 2 || state.writes != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportAllowDuplicateProceeds(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{duplicate: true})
	defer srv.Close()
	args := append(importArgs(path), "--allow-duplicate")
	out, _, err := runCLI(srv.URL, args...)
	if err != nil {
		t.Fatalf("allow duplicate import: %v", err)
	}
	if !strings.Contains(out, `"status": "imported"`) || state.writes != 1 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportJSONL(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, _ := newAttachmentImportServer(t, importServerOptions{})
	defer srv.Close()
	args := importArgs(path)
	args[0] = "--jsonl"
	out, _, err := runCLI(srv.URL, args...)
	if err != nil {
		t.Fatalf("JSONL import: %v", err)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("JSONL emitted multiple lines:\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode JSONL: %v", err)
	}
	if doc["kind"] != "attachment-import" {
		t.Fatalf("JSONL document = %#v", doc)
	}
}

func TestAttachmentImportHumanDryRunExplainsUpload(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, state := newAttachmentImportServer(t, importServerOptions{})
	defer srv.Close()
	out, _, err := runCLI(srv.URL,
		"attachment", "import", "--parent", "PARENT01", "--file", path, "--dry-run")
	if err != nil {
		t.Fatalf("human dry run: %v", err)
	}
	for _, want := range []string{"Status: planned", "Dry run", "upload and registration are required"} {
		if !strings.Contains(out, want) {
			t.Errorf("human dry run missing %q:\n%s", want, out)
		}
	}
	if state.writes != 0 || state.authorizations != 0 {
		t.Fatalf("human dry run mutated: %#v", state)
	}
}

func TestAttachmentImportUnknownCreateOutcomeIsPartialWithoutRetry(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{malformedCreateReply: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{`"status": "partial"`, `"stage": "metadata-create-unknown"`, `"code": "metadata-create-unknown"`, `"attachmentKey": null`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
	if state.writes != 1 || state.uploads != 0 || state.deletes != 0 {
		t.Fatalf("state = %#v", state)
	}
}

func TestAttachmentImportConflictingCreateResponseIsUnknown(t *testing.T) {
	for _, shape := range []string{"conflicting", "wrong-failure-index"} {
		t.Run(shape, func(t *testing.T) {
			path := writeAttachmentTestPDF(t)
			saveImportTestKey(t)
			srv, state := newAttachmentImportServer(t, importServerOptions{createReplyShape: shape})
			defer srv.Close()
			out, _, err := runCLI(srv.URL, importArgs(path)...)
			var exitErr cli.ExitCoder
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(out, `"code": "metadata-create-unknown"`) || state.writes != 1 || state.uploads != 0 {
				t.Fatalf("output=%s state=%#v", out, state)
			}
		})
	}
}

func TestAttachmentImportMetadataCreateFailureIsBounded(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	const secret = "server rejected key SECRET"
	srv, state := newAttachmentImportServer(t, importServerOptions{createUnauthorized: true, failureBody: secret})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out, `"status": "failed"`) || !strings.Contains(out, `"code": "metadata-create-failed"`) ||
		strings.Contains(out, secret) || state.writes != 1 || state.uploads != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportAuthorizeFailureReportsCreatedKey(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	const secret = "authorization detail SECRETUPLOADKEY"
	srv, state := newAttachmentImportServer(t, importServerOptions{
		authorizeStatus: http.StatusNotImplemented, failureBody: secret,
	})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{`"status": "partial"`, `"stage": "metadata-created"`, `"code": "upload-authorize-failed"`, `"attachmentKey": "NEWATT01"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, secret) || strings.Contains(out, "SECRETUPLOADKEY") || state.writes != 1 || state.uploads != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportPartialUploadFailure(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	const secretBody = "upload failed for http://local/api/local/uploads/SECRETUPLOADKEY"
	srv, state := newAttachmentImportServer(t, importServerOptions{
		uploadStatus: http.StatusBadRequest, failureBody: secretBody,
	})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v, want exit 1", err)
	}
	for _, want := range []string{`"status": "partial"`, `"stage": "authorized"`, `"code": "upload-failed"`, `"attachmentKey": "`} {
		if !strings.Contains(out, want) {
			t.Errorf("partial output missing %q:\n%s", want, out)
		}
	}
	if state.writes != 1 || state.uploads != 1 || state.registrations != 0 {
		t.Fatalf("partial state = %#v", state)
	}
	if strings.Contains(out, secretBody) || strings.Contains(out, "SECRETUPLOADKEY") {
		t.Fatalf("partial output leaked response body: %s", out)
	}
}

func TestAttachmentImportRegistrationErrorRecoveredByVerification(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, _ := newAttachmentImportServer(t, importServerOptions{
		registerStatus: http.StatusInternalServerError, registerDespiteError: true,
	})
	defer srv.Close()
	out, errOut, err := runCLI(srv.URL, importArgs(path)...)
	if err != nil {
		t.Fatalf("recovered import: %v", err)
	}
	if !strings.Contains(out, `"status": "imported"`) || !strings.Contains(out, `"stage": "verified"`) {
		t.Fatalf("recovered output = %s", out)
	}
	if !strings.Contains(errOut, "verification confirmed") {
		t.Fatalf("warning output = %s", errOut)
	}
}

func TestAttachmentImportRegistrationFailureIsPartialWithoutRollback(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, state := newAttachmentImportServer(t, importServerOptions{registerStatus: http.StatusInternalServerError})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out, `"status": "partial"`) || !strings.Contains(out, `"stage": "uploaded"`) ||
		!strings.Contains(out, `"code": "register-failed"`) || !strings.Contains(out, `"attachmentKey": "NEWATT01"`) ||
		state.writes != 1 || state.uploads != 1 || state.registrations != 1 || state.deletes != 0 {
		t.Fatalf("output=%s state=%#v", out, state)
	}
}

func TestAttachmentImportVerificationFailure(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	saveImportTestKey(t)
	srv, _ := newAttachmentImportServer(t, importServerOptions{wrongVerificationSize: true})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v, want exit 1", err)
	}
	if !strings.Contains(out, `"stage": "registered"`) || !strings.Contains(out, `"code": "verification-failed"`) || !strings.Contains(out, `"size": false`) {
		t.Fatalf("verification output = %s", out)
	}
}

func TestAttachmentImportRejectsInvalidParentTypes(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	for _, itemType := range []string{"attachment", "note", "annotation"} {
		t.Run(itemType, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"key": "PARENT01", "data": map[string]any{"itemType": itemType},
				})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			_, _, err := runCLI(srv.URL,
				"attachment", "import", "--parent", "PARENT01", "--file", path, "--dry-run")
			if err == nil || !strings.Contains(err.Error(), "cannot be a bibliographic") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// #80: the duplicate scan must read only the parent's children (the /children
// route), never the unscoped /items route where Zotero drops an itemKey scope
// once itemType is added and returns the whole library. A stray foreign-parent
// record must be skipped, not turned into a fatal error.
func TestAttachmentImportDuplicateScanUsesChildrenRouteOnly(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"PARENT01","data":{"key":"PARENT01","itemType":"journalArticle","title":"Paper"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("itemType") != "attachment" {
			t.Errorf("children query = %s", r.URL.RawQuery)
		}
		// A stray attachment under a different parent, as a scope leak might yield.
		// The fix must skip it defensively rather than abort.
		_, _ = w.Write([]byte(`[{"key":"FOREIGN1","version":"","links":{},"data":{"key":"FOREIGN1","itemType":"attachment","parentItem":"OTHERXXX","linkMode":"imported_file","contentType":"application/pdf","filename":"other.pdf","tags":[],"md5":null,"mtime":null}}]`))
	})
	mux.HandleFunc("GET /api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("duplicate scan hit the unscoped /items route (%s); it must use /items/PARENT01/children", r.URL.RawQuery)
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, _, err := runCLI(srv.URL, "--json", "attachment", "import",
		"--parent", "PARENT01", "--file", path, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run import aborted on a foreign attachment (#80 regression): %v", err)
	}
	if !strings.Contains(out, `"status": "planned"`) {
		t.Fatalf("expected planned, got %s", out)
	}
}

// #82: with --yes there is no prompt to inform, so the plan block must not
// pre-echo fields the outcome block already reports — on a preflight failure
// that was pure duplication (Parent/MD5 twice).
func TestAttachmentImportYesFailureDoesNotDuplicateFields(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	mux := http.NewServeMux()
	// No Zotero-Server-ID anywhere: the build has no write API, so the import
	// fails at the capability check in preflight, before anything is attempted.
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("Nothing")) })
	mux.HandleFunc("GET /api/users/0/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"PARENT01","data":{"key":"PARENT01","itemType":"journalArticle","title":"Paper"}}`))
	})
	mux.HandleFunc("GET /api/users/0/items/PARENT01/children", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, _, err := runCLI(srv.URL, "attachment", "import", "--parent", "PARENT01", "--file", path, "--yes")
	if err == nil {
		t.Fatal("expected a failure on a build without the write API")
	}
	if n := strings.Count(out, "Parent:"); n != 1 {
		t.Errorf("Parent: appears %d times, want 1 (plan block must not duplicate the outcome):\n%s", n, out)
	}
}

func TestAttachmentImportGroupDryRunUsesGroupRoutes(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/groups", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":42,"data":{"id":42,"name":"Research Group"}}]`))
	})
	mux.HandleFunc("GET /api/groups/42/items/PARENT01", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"PARENT01","data":{"key":"PARENT01","itemType":"journalArticle"}}`))
	})
	mux.HandleFunc("GET /api/groups/42/items/PARENT01/children", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("itemType") != "attachment" {
			t.Errorf("group duplicate query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "-L", "Research Group", "--json", "attachment", "import",
		"--parent", "PARENT01", "--file", path, "--dry-run")
	if err != nil {
		t.Fatalf("group dry run: %v", err)
	}
	if !strings.Contains(out, `"type": "group"`) || !strings.Contains(out, `"id": 42`) || !strings.Contains(out, `"status": "planned"`) {
		t.Fatalf("group output = %s", out)
	}
}

// A non-interactive import with no write lease fails closed before any write or
// authorization, reporting the missing lease.
func TestAttachmentImportWithoutLeaseFailsClosed(t *testing.T) {
	path := writeAttachmentTestPDF(t)
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir()) // config dir set, but no lease seeded
	srv, state := newAttachmentImportServer(t, importServerOptions{})
	defer srv.Close()
	out, _, err := runCLI(srv.URL, importArgs(path)...)
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out, `"code": "authorization-required"`) || !strings.Contains(out, "no active write lease") {
		t.Fatalf("output = %s", out)
	}
	if state.authorizations != 0 || state.writes != 0 {
		t.Fatalf("a lease-less import touched Zotero: %#v", state)
	}
}

// Interactive import is the human's own authority but still needs "Always Allow":
// the multi-phase flow reuses the key, and a single-use grant would be spent on
// the first write. ensureRememberedLocalKey enforces this directly.
func TestImportInteractiveRequiresAlwaysAllow(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	srv, _ := newAttachmentImportServer(t, importServerOptions{oneTimeAuthorization: true})
	defer srv.Close()
	client := zotero.New(srv.URL)
	cmd := &cli.Command{ErrWriter: io.Discard}
	err := ensureRememberedLocalKey(context.Background(), cmd, client)
	if err == nil || !strings.Contains(err.Error(), "Always Allow") {
		t.Fatalf("err = %v, want an Always-Allow requirement", err)
	}
}

func TestAttachmentImportRejectsRawAndMachineMutationWithoutYesBeforeIO(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "raw", args: []string{"--raw", "attachment", "import", "--parent", "PARENT01", "--file", "/missing.pdf"}, want: "no raw Zotero response"},
		{name: "machine yes", args: []string{"--json", "attachment", "import", "--parent", "PARENT01", "--file", "/missing.pdf"}, want: "require --yes"},
		{name: "web", args: []string{"--web", "attachment", "import", "--parent", "PARENT01", "--file", "/missing.pdf", "--yes"}, want: "local-only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runCLI("http://127.0.0.1:1", tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestStageAttachmentFileValidation(t *testing.T) {
	t.Run("detects PNG despite filename", func(t *testing.T) {
		path := writeAttachmentTestFile(t, "misleading.pdf", testPNG)
		staged, err := stageAttachmentFile(path, "renamed.pdf", "")
		if err != nil {
			t.Fatalf("stageAttachmentFile: %v", err)
		}
		defer staged.close()
		if staged.filename != "renamed.pdf" || staged.contentType != "image/png" || staged.size != int64(len(testPNG)) || len(staged.md5) != 32 || staged.mtime != 1700000000000 {
			t.Fatalf("staged = %#v", staged)
		}
	})
	t.Run("explicit content type overrides and canonicalizes", func(t *testing.T) {
		path := writeAttachmentTestFile(t, "image.png", testPNG)
		staged, err := stageAttachmentFile(path, "", ` IMAGE/PNG; Profile="custom" `)
		if err != nil {
			t.Fatalf("stageAttachmentFile: %v", err)
		}
		defer staged.close()
		if staged.contentType != "image/png; profile=custom" {
			t.Fatalf("content type = %q", staged.contentType)
		}
	})
	t.Run("unknown bytes use octet stream despite extension", func(t *testing.T) {
		content := []byte{0x00, 0x01, 0x02, 0x03, 0xff}
		path := writeAttachmentTestFile(t, "unknown.png", content)
		staged, err := stageAttachmentFile(path, "", "")
		if err != nil {
			t.Fatalf("stageAttachmentFile: %v", err)
		}
		defer staged.close()
		if staged.contentType != "application/octet-stream" {
			t.Fatalf("content type = %q", staged.contentType)
		}
	})
	for _, value := range []string{"not a mime", "foo", "image/", "image/*", "image/*+json", "foo/**", "text/plain; charset"} {
		if _, err := stageAttachmentFile("/missing", "", value); err == nil || !strings.Contains(err.Error(), "invalid --content-type") {
			t.Errorf("invalid content type %q: %v", value, err)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stageAttachmentFile(empty, "", ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := stageAttachmentFile(t.TempDir(), "", ""); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
	t.Run("missing source redacts path", func(t *testing.T) {
		secretPath := filepath.Join(t.TempDir(), "private-source.bin")
		_, err := stageAttachmentFile(secretPath, "", "")
		if err == nil || strings.Contains(err.Error(), secretPath) || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("oversized source", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "oversized.bin")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(zotero.MaxAttachmentFileSize + 1); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := stageAttachmentFile(path, "", ""); err == nil || !strings.Contains(err.Error(), "safety limit") {
			t.Fatalf("oversized error = %v", err)
		}
	})
	for _, filename := range []string{
		"../bad.pdf", "report?.png", "CON.pdf", "lpt9.PNG", "COM¹.pdf", "LPT³.txt",
		"control\u007f.pdf", "control\u0085.png", "trailing. ", strings.Repeat("a", 252) + ".pdf",
	} {
		if _, err := attachmentImportFilename("paper.pdf", filename); err == nil {
			t.Errorf("invalid filename accepted: %q", filename)
		}
	}
	for _, filename := range []string{"paper.pdf", "image.png", "résumé.txt", "data[final].bin"} {
		if got, err := attachmentImportFilename("source.bin", filename); err != nil || got != filename {
			t.Errorf("valid filename %q = %q, %v", filename, got, err)
		}
	}
}

func TestStagedAttachmentIsIndependentOfSource(t *testing.T) {
	original := []byte(testPDF)
	path := writeAttachmentTestFile(t, "paper.pdf", original)
	staged, err := stageAttachmentFile(path, "", "")
	if err != nil {
		t.Fatalf("stageAttachmentFile: %v", err)
	}
	stagedPath := staged.file.Name()
	if err := os.WriteFile(path, testPNG, 0o600); err != nil {
		t.Fatalf("replace source: %v", err)
	}
	if _, err := staged.file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(staged.file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("staged bytes changed with source: %q", got)
	}
	staged.close()
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private staging file still exists: %v", err)
	}
}

func TestValidateAttachmentSourceURL(t *testing.T) {
	for _, value := range []string{"", "https://example.com/paper.pdf", "http://example.com"} {
		if err := validateAttachmentSourceURL(value); err != nil {
			t.Errorf("valid URL %q: %v", value, err)
		}
	}
	for _, value := range []string{"file:///tmp/paper.pdf", "relative", "https://user:pass@example.com/paper.pdf"} {
		if err := validateAttachmentSourceURL(value); err == nil {
			t.Errorf("invalid URL accepted: %q", value)
		}
	}
}

func TestAttachmentItemCreateGuidance(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"itemType":"attachment","linkMode":"imported_file","path":"/tmp/paper.pdf"}`),
		json.RawMessage(`{"itemType":"attachment","linkMode":"imported_file","filename":"paper.pdf"}`),
	} {
		if _, err := attachmentItemCreateGuidance([]json.RawMessage{raw}); err == nil || !strings.Contains(err.Error(), "attachment import") {
			t.Fatalf("guidance error = %v", err)
		}
	}
	warnings, err := attachmentItemCreateGuidance([]json.RawMessage{
		json.RawMessage(`{"itemType":"attachment","linkMode":"imported_file"}`),
	})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "metadata only") {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
}

func TestAttachmentImportCommandHelp(t *testing.T) {
	out, _, err := runCLI("http://127.0.0.1:1", "attachment", "import", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"local file", "MIME type is detected", "--parent", "--file", "--content-type", "--source-url", "not downloaded", "Always Allow", "--dry-run", "--allow-duplicate"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--url <SOURCE") {
		t.Fatalf("help ambiguously reused global --url:\n%s", out)
	}
}

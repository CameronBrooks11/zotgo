//go:build live

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestLiveAttachmentImportPDFAndPNG(t *testing.T) {
	base := os.Getenv("ZOTGO_BASE_URL")
	if base == "" {
		base = "http://localhost:23119"
	}
	client := zotero.New(base)
	ctx := context.Background()
	health := client.CheckHealth(ctx)
	if !health.Ready() {
		t.Skipf("Zotero is not ready: %+v", health)
	}
	if !health.Supports(zotero.CapabilityWrite) {
		t.Skip("this Zotero build has no local write API; run against a write-capable build")
	}

	t.Log("Authorizing — click **Always Allow** in Zotero's prompt…")
	remember, err := client.Authorize(ctx, "zotgo live attachment import")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !remember {
		t.Fatal("got a single-use key (you clicked \"Allow\"); this test needs a persistent key — re-run and click \"Always Allow\"")
	}

	// Parent create and cleanup are test scaffolding driven directly through this
	// client, so it is its own authority; the import under test runs through the
	// CLI, gated by a seeded lease carrying the same authorized key.
	client.SetWriteAuthorizer(zotero.AllowAllWrites())
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	seedLiveImportLease(t, client.LocalKey())

	library := zotero.UserLibrary()
	marker := fmt.Sprintf("zotgo live managed attachment import %d", time.Now().UnixNano())
	parentKey := ""
	defer cleanupLiveAttachmentImport(t, client, library, marker, &parentKey)
	parentBody, err := json.Marshal(map[string]string{"itemType": "journalArticle", "title": marker})
	if err != nil {
		t.Fatal(err)
	}
	parentResult, err := client.CreateItemsReturningKeys(ctx, zotero.OpItemCreate, library, []json.RawMessage{parentBody})
	if err != nil || len(parentResult.Failed) != 0 {
		t.Fatalf("create parent: result=%+v err=%v", parentResult, err)
	}
	parentKey = parentResult.Successful["0"]
	if parentKey == "" {
		t.Fatalf("create parent returned no key: %+v", parentResult)
	}

	cases := []struct {
		name        string
		localName   string
		managedName string
		content     []byte
		contentType string
	}{
		{
			name: "PDF", localName: "paper.pdf", managedName: "live paper+resume 100%.pdf",
			content: []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"), contentType: "application/pdf",
		},
		{
			name: "PNG", localName: "image.png", managedName: "live image+résumé 100%.png",
			content:     []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"),
			contentType: "image/png",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), test.localName)
			if err := os.WriteFile(source, test.content, 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			mtime := time.UnixMilli(1700000000000)
			if err := os.Chtimes(source, mtime, mtime); err != nil {
				t.Fatalf("set source mtime: %v", err)
			}

			args := []string{
				"--json", "attachment", "import", "--parent", parentKey,
				"--file", source, "--filename", test.managedName, "--title", "Live " + test.name,
				"--source-url", "https://example.com/zotgo-live-" + test.localName, "--yes",
			}
			out, _, runErr := runCLI(client.BaseURL(), args...)
			var document struct {
				Kind string `json:"kind"`
				Data struct {
					Status        string  `json:"status"`
					AttachmentKey *string `json:"attachmentKey"`
					Verification  struct {
						Parent, ManagedStorage, Title, SourceURL bool
						Filename, ContentType, Size, Checksum    bool
						ActualFilename                           string `json:"actualFilename"`
					} `json:"verification"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &document); err != nil {
				t.Fatalf("decode command output: %v\n%s", err, out)
			}
			if runErr != nil {
				t.Fatalf("attachment import: %v\n%s", runErr, out)
			}
			if document.Kind != "attachment-import" || document.Data.Status != "imported" || document.Data.AttachmentKey == nil {
				t.Fatalf("document = %+v", document)
			}
			verification := document.Data.Verification
			if !verification.Parent || !verification.ManagedStorage || !verification.Title || !verification.SourceURL ||
				!verification.Filename || !verification.ContentType || !verification.Size || !verification.Checksum ||
				verification.ActualFilename != test.managedName {
				t.Fatalf("verification = %+v", verification)
			}

			attachment, err := client.Attachment(ctx, library, *document.Data.AttachmentKey)
			if err != nil {
				t.Fatalf("read imported attachment: %v", err)
			}
			if attachment.ParentKey != parentKey || attachment.LinkMode != "imported_file" ||
				attachment.Filename != test.managedName || attachment.ContentType != test.contentType || attachment.MD5 == nil {
				t.Fatalf("imported attachment = %+v", attachment)
			}

			duplicateOut, _, duplicateErr := runCLI(client.BaseURL(), args...)
			if duplicateErr != nil {
				t.Fatalf("duplicate check: %v\n%s", duplicateErr, duplicateOut)
			}
			var duplicate struct {
				Data struct {
					Status        string  `json:"status"`
					AttachmentKey *string `json:"attachmentKey"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(duplicateOut), &duplicate); err != nil {
				t.Fatalf("decode duplicate output: %v\n%s", err, duplicateOut)
			}
			if duplicate.Data.Status != "duplicate" || duplicate.Data.AttachmentKey == nil ||
				*duplicate.Data.AttachmentKey != *document.Data.AttachmentKey {
				t.Fatalf("duplicate result = %+v", duplicate.Data)
			}
		})
	}
}

func cleanupLiveAttachmentImport(t *testing.T, client *zotero.Client, library zotero.LibraryRef, marker string, parentKey *string) {
	t.Helper()
	ctx := context.Background()
	parents := make([]string, 0, 1)
	if *parentKey != "" {
		parents = append(parents, *parentKey)
	} else {
		items, err := client.AllItems(ctx, library, zotero.ItemsOptions{Query: marker, Everything: true, Limit: 100})
		if err != nil {
			t.Errorf("discover cleanup parent: %v", err)
			return
		}
		for _, envelope := range items {
			data, err := envelope.ItemData()
			if err == nil && data.ItemType == "journalArticle" && data.Title == marker {
				parents = append(parents, envelope.Key)
			}
		}
	}
	if len(parents) == 0 {
		return
	}

	keys := make([]string, 0, len(parents)+2)
	seen := make(map[string]bool)
	for _, parent := range parents {
		children, err := client.AllItems(ctx, library, zotero.ItemsOptions{
			ItemKeys: []string{parent}, ItemType: "attachment", Limit: 100,
		})
		if err != nil {
			t.Errorf("discover cleanup children for %s: %v", parent, err)
		} else {
			for _, child := range children {
				if !seen[child.Key] {
					seen[child.Key] = true
					keys = append(keys, child.Key)
				}
			}
		}
		if !seen[parent] {
			seen[parent] = true
			keys = append(keys, parent)
		}
	}
	version, err := client.LibraryVersion(ctx, library)
	if err != nil {
		t.Errorf("cleanup library version: %v", err)
		return
	}
	if err := client.DeleteItems(ctx, zotero.OpItemDelete, library, keys, version); err != nil {
		t.Errorf("cleanup items %v: %v", keys, err)
	}
}

// seedLiveImportLease writes a lease permitting attachment.import, carrying the
// live-authorized key, into the test's config dir so the CLI import path is
// authorized non-interactively.
func seedLiveImportLease(t *testing.T, key string) {
	t.Helper()
	l := &lease{
		ID:       "lease_live_import",
		Created:  time.Now(),
		Expires:  time.Now().Add(30 * time.Minute),
		Scope:    leaseScope{Libraries: []string{libraryToken(zotero.UserLibrary())}, Operations: []string{string(zotero.OpAttachmentImport)}},
		WriteKey: key,
	}
	if err := saveLease(l); err != nil {
		t.Fatalf("saveLease: %v", err)
	}
}

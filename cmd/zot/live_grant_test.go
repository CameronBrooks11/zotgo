//go:build live

// Live grant tests exercise the write-lease path against a real, write-capable
// Zotero (zotero/zotero#5015). They mutate the library, so point them at a
// THROWAWAY profile, never your real one:
//
//	ZOTGO_BASE_URL=http://localhost:23119 go test -tags live ./cmd/zot -run TestLiveGrant -v
//
// Authorize pops a modal in Zotero — click **Always Allow** (a single-use "Allow"
// key is consumed by the first write, and this drives several).
package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// TestLiveGrantedWriteRoundTrip proves the end-to-end lease path against real
// Zotero: a lease that carries the authorized key lets a non-interactive write
// through, and revoking it makes the next one fail closed.
func TestLiveGrantedWriteRoundTrip(t *testing.T) {
	base := os.Getenv("ZOTGO_BASE_URL")
	if base == "" {
		base = "http://localhost:23119"
	}
	c := zotero.New(base)
	ctx := context.Background()
	h := c.CheckHealth(ctx)
	if !h.Ready() {
		t.Skipf("Zotero not ready: %+v", h)
	}
	if !h.Supports(zotero.CapabilityWrite) {
		t.Skip("this Zotero build has no local write API; run against a write-capable build")
	}

	t.Log("Authorizing — click **Always Allow** in Zotero's prompt…")
	remember, err := c.Authorize(ctx, "zotgo grant live test")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !remember {
		t.Fatal("got a single-use key (you clicked \"Allow\"); this test needs a persistent key — re-run and click \"Always Allow\"")
	}

	// Seed a real lease (carrying the authorized key) into an isolated config dir,
	// then drive the CLI non-interactively so it takes the lease path.
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	l := &lease{
		ID:      "lease_live",
		Created: time.Now(),
		Expires: time.Now().Add(10 * time.Minute),
		Scope: leaseScope{
			Libraries:  []string{libraryToken(zotero.UserLibrary())},
			Operations: []string{string(zotero.OpItemCreate), string(zotero.OpItemDelete)},
		},
		WriteKey: c.LocalKey(),
	}
	if err := saveLease(l); err != nil {
		t.Fatalf("saveLease: %v", err)
	}

	// A leased create succeeds.
	file := itemInputFile(t, `{"itemType":"book","title":"zotgo lease live test"}`)
	out, _, err := runCLI(base, "--json", "item", "create", "--file", file, "--yes")
	if err != nil {
		t.Fatalf("leased create failed: %v", err)
	}
	doc := decodeItemMutationDocument(t, out)
	if len(doc.Data) != 1 || doc.Data[0].Status != "created" || doc.Data[0].Key == "" {
		t.Fatalf("create document = %+v", doc)
	}
	created := doc.Data[0].Key
	t.Logf("created %s under lease %s", created, l.ID)

	// A leased delete cleans it up (and exercises a second scoped operation).
	if _, _, err := runCLI(base, "item", "delete", created, "--yes"); err != nil {
		t.Fatalf("leased delete failed: %v", err)
	}

	// After revocation, the next non-interactive write fails closed.
	if _, _, err := runCLI(base, "grant", "revoke"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := runCLI(base, "item", "create", "--file", file, "--yes"); err == nil || !strings.Contains(err.Error(), "no active write lease") {
		t.Fatalf("post-revoke write err = %v, want a no-active-lease refusal", err)
	}
}

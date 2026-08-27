package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// withStdinContent points os.Stdin at a regular file holding content for the test,
// so isTerminal reports false (a pipe/redirect, not a terminal) deterministically
// and prompts read the given bytes.
func withStdinContent(t *testing.T, content string) {
	t.Helper()
	f, err := os.Open(itemInputFile(t, content))
	if err != nil {
		t.Fatalf("open stdin stand-in: %v", err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; f.Close() })
}

// withNonTTYStdin points os.Stdin at an empty regular file, so isTerminal reports
// false regardless of how `go test` was launched.
func withNonTTYStdin(t *testing.T) { withStdinContent(t, "") }

// A persisted local key is reused so an interactive write does not re-prompt
// Zotero's modal on every run. authorizeInteractively must load it, not rely on
// the in-memory key alone (which is empty in a fresh process).
func TestAuthorizeInteractivelyReusesPersistedKey(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	if err := saveLocalKey("PERSISTED"); err != nil {
		t.Fatalf("saveLocalKey: %v", err)
	}
	// An unreachable URL: if the persisted key were ignored, Authorize would dial
	// it and fail, so success proves the key was reused without re-prompting.
	c := zotero.New("http://127.0.0.1:0")
	if err := authorizeInteractively(context.Background(), c); err != nil {
		t.Fatalf("authorizeInteractively: %v", err)
	}
	if c.LocalKey() != "PERSISTED" {
		t.Errorf("LocalKey = %q, want the reused persisted key", c.LocalKey())
	}
}

// A scripted "y" over a pipe is not an interactive human: it must go through the
// lease path (and thus be refused without one), not install allow-all.
func TestPipedYesRequiresLease(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	withStdinContent(t, "y\n")
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()

	// Human mode, no --yes: confirm() reads "y" from the pipe, but the write is
	// still non-interactive, so with no lease it fails closed.
	_, _, err := runCLI(srv.URL, "item", "delete", "ITEM0001")
	if err == nil || !strings.Contains(err.Error(), "no active write lease") {
		t.Fatalf("err = %v, want a no-active-lease refusal for piped 'y'", err)
	}
	if fake.deletes.Load() != 0 {
		t.Errorf("a piped-'y' delete reached Zotero (%d deletes) — allow-all leaked", fake.deletes.Load())
	}
}

// grant mints a lease only for an interactive human, so with no terminal it must
// refuse rather than hang or mint one non-interactively.
func TestGrantRefusesNonInteractive(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	withNonTTYStdin(t)
	_, _, err := runCLI("http://127.0.0.1:0", "grant", "--ttl", "10m")
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("err = %v, want an interactive-terminal refusal", err)
	}
}

func TestGrantStatusNoLease(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	out, _, err := runCLI("http://127.0.0.1:0", "grant", "status")
	if err != nil {
		t.Fatalf("grant status: %v", err)
	}
	if !strings.Contains(out, "No active write lease") {
		t.Errorf("status output = %q", out)
	}
}

func TestGrantStatusAndRevoke(t *testing.T) {
	seedLeaseScoped(t, time.Now().Add(time.Hour), zotero.OpItemPatch)

	out, _, err := runCLI("http://127.0.0.1:0", "grant", "status")
	if err != nil {
		t.Fatalf("grant status: %v", err)
	}
	for _, want := range []string{"lease_scoped", "user:0", "item.patch", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q in:\n%s", want, out)
		}
	}

	if _, _, err := runCLI("http://127.0.0.1:0", "grant", "revoke"); err != nil {
		t.Fatalf("grant revoke: %v", err)
	}
	out, _, err = runCLI("http://127.0.0.1:0", "grant", "status")
	if err != nil {
		t.Fatalf("grant status after revoke: %v", err)
	}
	if !strings.Contains(out, "No active write lease") {
		t.Errorf("lease survived revoke:\n%s", out)
	}
}

// A non-interactive write with no lease fails closed, before any write reaches
// Zotero.
func TestNonInteractiveWriteRefusedWithoutLease(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()

	file := itemInputFile(t, `{"itemType":"book","title":"X"}`)
	_, _, err := runCLI(srv.URL, "item", "create", "--file", file, "--yes")
	if err == nil || !strings.Contains(err.Error(), "no active write lease") {
		t.Fatalf("err = %v, want a no-active-lease refusal", err)
	}
	if fake.writes.Load() != 0 {
		t.Errorf("a lease-less write reached Zotero (%d writes)", fake.writes.Load())
	}
}

func TestNonInteractiveWriteRefusedWhenExpired(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	seedLeaseScoped(t, time.Now().Add(-time.Minute), zotero.OpItemCreate) // already expired

	file := itemInputFile(t, `{"itemType":"book","title":"X"}`)
	_, _, err := runCLI(srv.URL, "item", "create", "--file", file, "--yes")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want an expired-lease refusal", err)
	}
	if fake.writes.Load() != 0 {
		t.Errorf("an expired-lease write reached Zotero (%d writes)", fake.writes.Load())
	}
}

// The operation the write path declares must be the command's own, so a lease
// that omits it refuses. This is the end-to-end guard against a swapped operation
// on the two shared-dispatch methods (PatchItem backs item.patch and tag add/
// remove; PatchCollection backs rename and move).
func TestWriteRefusedForUnpermittedOperation(t *testing.T) {
	t.Run("item patch is not tag.add", func(t *testing.T) {
		fake := &itemWriteFake{}
		srv := newItemWriteFake(t, fake)
		defer srv.Close()
		seedLeaseScoped(t, time.Now().Add(time.Hour), zotero.OpTagAdd) // lease permits tag.add only

		file := itemInputFile(t, `{"title":"New"}`)
		_, _, err := runCLI(srv.URL, "item", "patch", "ITEM0001", "--file", file, "--yes")
		if err == nil || !strings.Contains(err.Error(), "item.patch") {
			t.Fatalf("err = %v, want a refusal naming item.patch", err)
		}
		if fake.patches.Load() != 0 {
			t.Error("patch reached Zotero despite an out-of-scope lease")
		}
	})

	t.Run("tag add is not item.patch", func(t *testing.T) {
		fake := &itemWriteFake{}
		srv := newItemWriteFake(t, fake)
		defer srv.Close()
		seedLeaseScoped(t, time.Now().Add(time.Hour), zotero.OpItemPatch) // lease permits item.patch only

		_, _, err := runCLI(srv.URL, "tag", "add", "newtag", "--item", "ITEM0001", "--yes")
		if err == nil || !strings.Contains(err.Error(), "tag.add") {
			t.Fatalf("err = %v, want a refusal naming tag.add", err)
		}
		if fake.patches.Load() != 0 {
			t.Error("tag add reached Zotero despite an out-of-scope lease")
		}
	})

	t.Run("collection move is not collection.rename", func(t *testing.T) {
		fake := &collectionWriteFake{}
		srv := newCollectionWriteFake(t, fake)
		defer srv.Close()
		seedLeaseScoped(t, time.Now().Add(time.Hour), zotero.OpCollectionRename) // permits rename only

		_, _, err := runCLI(srv.URL, "collection", "move", "COLL0001", "--to-top", "--yes")
		if err == nil || !strings.Contains(err.Error(), "collection.move") {
			t.Fatalf("err = %v, want a refusal naming collection.move", err)
		}
		if fake.patches.Load() != 0 {
			t.Error("collection move reached Zotero despite an out-of-scope lease")
		}
	})
}

// Every decision — including a refusal — is written to the lease's audit log, and
// grant status summarizes it.
func TestAuditRecordsRefusalAndStatusCounts(t *testing.T) {
	fake := &itemWriteFake{}
	srv := newItemWriteFake(t, fake)
	defer srv.Close()
	seedLeaseScoped(t, time.Now().Add(time.Hour), zotero.OpItemPatch) // no item.delete

	// A delete is out of scope: refused and audited.
	if _, _, err := runCLI(srv.URL, "item", "delete", "ITEM0001", "--yes"); err == nil {
		t.Fatal("expected the delete to be refused")
	}
	allowed, refused := auditCounts("lease_scoped")
	if allowed != 0 || refused != 1 {
		t.Fatalf("audit counts = %d allowed / %d refused, want 0/1", allowed, refused)
	}

	out, _, err := runCLI(srv.URL, "grant", "status")
	if err != nil {
		t.Fatalf("grant status: %v", err)
	}
	if !strings.Contains(out, "0 allowed, 1 refused") {
		t.Errorf("status missing audit summary:\n%s", out)
	}
}

// A long-lived lease is the one a human is likeliest to forget about, so status
// must say so plainly rather than rendering it as a routine "active" lease.
func TestGrantStatusFlagsLongLivedLease(t *testing.T) {
	seedLeaseScoped(t, time.Now().Add(MaxLeaseTTL), zotero.OpItemPatch)

	out, _, err := runCLI("http://127.0.0.1:0", "grant", "status")
	if err != nil {
		t.Fatalf("grant status: %v", err)
	}
	for _, want := range []string{"LONG-LIVED", "grant revoke"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q for a month-long lease in:\n%s", want, out)
		}
	}
}

// The flag is for long grants only: a default 30-minute lease must not be
// dressed up as something the user needs to go clean up.
func TestGrantStatusDoesNotFlagShortLease(t *testing.T) {
	seedLeaseScoped(t, time.Now().Add(DefaultLeaseTTL), zotero.OpItemPatch)

	out, _, err := runCLI("http://127.0.0.1:0", "grant", "status")
	if err != nil {
		t.Fatalf("grant status: %v", err)
	}
	if strings.Contains(out, "LONG-LIVED") {
		t.Errorf("a 30-minute lease was flagged long-lived:\n%s", out)
	}
}

// The extra confirmation is the whole friction budget for a long lease, so it
// must fire above the threshold, stay silent at or below it, and treat anything
// but an explicit yes as a refusal.
func TestConfirmLongLease(t *testing.T) {
	expires := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		ttl        time.Duration
		answer     string
		want       bool
		wantPrompt bool
	}{
		{"default TTL is not prompted", DefaultLeaseTTL, "", true, false},
		{"threshold itself is not prompted", LongLeaseTTL, "", true, false},
		{"just past the threshold, accepted", LongLeaseTTL + time.Minute, "y\n", true, true},
		{"a month, accepted", MaxLeaseTTL, "yes\n", true, true},
		{"a month, declined", MaxLeaseTTL, "n\n", false, true},
		{"a month, empty answer defaults to no", MaxLeaseTTL, "\n", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf strings.Builder
			got := confirmLongLease(strings.NewReader(tc.answer), &buf, tc.ttl, expires)
			if got != tc.want {
				t.Errorf("confirmLongLease(%s, %q) = %v, want %v", tc.ttl, tc.answer, got, tc.want)
			}
			if prompted := strings.Contains(buf.String(), "long-lived"); prompted != tc.wantPrompt {
				t.Errorf("prompted = %v, want %v; output:\n%s", prompted, tc.wantPrompt, buf.String())
			}
			// The point of the second prompt is the concrete date, not a duration.
			if tc.wantPrompt && !strings.Contains(buf.String(), "2026-09-26T12:00:00Z") {
				t.Errorf("prompt did not name the end date:\n%s", buf.String())
			}
		})
	}
}

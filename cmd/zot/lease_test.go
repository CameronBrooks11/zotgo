package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestLibraryToken(t *testing.T) {
	cases := map[string]struct {
		lib  zotero.LibraryRef
		want string
	}{
		"local user (sentinel)": {zotero.UserLibrary(), "user:0"},
		"group":                 {zotero.GroupLibrary(12345, "Team"), "group:12345"},
		"web user (real id)":    {zotero.LibraryRef{Kind: zotero.LibraryKindUser, ID: 8784047}, "user:8784047"},
	}
	for name, tc := range cases {
		if got := libraryToken(tc.lib); got != tc.want {
			t.Errorf("%s: libraryToken = %q, want %q", name, got, tc.want)
		}
	}
}

func TestLeaseAuthorize(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	base := &lease{
		Expires: now.Add(30 * time.Minute),
		Scope: leaseScope{
			Libraries:  []string{"user:0"},
			Operations: []string{"item.patch", "item.create"},
		},
	}
	cases := []struct {
		name string
		op   zotero.Operation
		lib  zotero.LibraryRef
		at   time.Time
		want error
	}{
		{"in scope", zotero.OpItemPatch, zotero.UserLibrary(), now, nil},
		{"expired", zotero.OpItemPatch, zotero.UserLibrary(), now.Add(time.Hour), ErrLeaseExpired},
		{"wrong library", zotero.OpItemPatch, zotero.GroupLibrary(5, "G"), now, ErrLeaseLibrary},
		{"wrong operation", zotero.OpItemDelete, zotero.UserLibrary(), now, ErrLeaseOperation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := base.authorize(tc.op, tc.lib, tc.at)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("authorize = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("authorize = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestResolveGrantOperations(t *testing.T) {
	// Default withholds the three destructive operations.
	def, err := resolveGrantOperations(nil)
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	for _, deleteOp := range []string{"item.delete", "collection.delete", "tag.delete"} {
		if contains(def, deleteOp) {
			t.Errorf("default grant includes destructive %q", deleteOp)
		}
	}
	for _, safeOp := range []string{"item.create", "item.patch", "tag.add", "attachment.import"} {
		if !contains(def, safeOp) {
			t.Errorf("default grant omits non-destructive %q", safeOp)
		}
	}

	// Explicit list is validated, de-duplicated, and sorted.
	got, err := resolveGrantOperations([]string{"item.patch", "item.create", "item.patch"})
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if strings.Join(got, ",") != "item.create,item.patch" {
		t.Errorf("explicit = %v, want [item.create item.patch] sorted & deduped", got)
	}

	// An unknown operation is rejected rather than silently dropped.
	if _, err := resolveGrantOperations([]string{"item.destroy"}); err == nil {
		t.Error("unknown operation accepted")
	}
}

func TestLeaseSaveLoadRevoke(t *testing.T) {
	t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())

	if _, err := loadActiveLease(); !errors.Is(err, ErrNoActiveLease) {
		t.Fatalf("fresh config: loadActiveLease = %v, want ErrNoActiveLease", err)
	}

	want := &lease{
		ID:       "lease_abc123",
		Created:  time.Now().UTC().Truncate(time.Second),
		Expires:  time.Now().UTC().Add(time.Hour).Truncate(time.Second),
		Scope:    leaseScope{Libraries: []string{"user:0"}, Operations: []string{"item.patch"}},
		WriteKey: "K",
		Note:     "hello",
	}
	if err := saveLease(want); err != nil {
		t.Fatalf("saveLease: %v", err)
	}
	got, err := loadActiveLease()
	if err != nil {
		t.Fatalf("loadActiveLease: %v", err)
	}
	if got.ID != want.ID || got.WriteKey != want.WriteKey || got.Note != want.Note ||
		strings.Join(got.Scope.Operations, ",") != "item.patch" || !got.Expires.Equal(want.Expires) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if err := revokeLease(); err != nil {
		t.Fatalf("revokeLease: %v", err)
	}
	if _, err := loadActiveLease(); !errors.Is(err, ErrNoActiveLease) {
		t.Fatalf("after revoke: loadActiveLease = %v, want ErrNoActiveLease", err)
	}
	// Revoking again is not an error.
	if err := revokeLease(); err != nil {
		t.Errorf("revoke on missing lease errored: %v", err)
	}
}

// seedLeaseScoped writes a lease with the given expiry and operations into the
// test's config dir, for enforcement tests that need a specific scope.
func seedLeaseScoped(t *testing.T, expires time.Time, ops ...zotero.Operation) {
	t.Helper()
	if os.Getenv("ZOTGO_CONFIG_DIR") == "" {
		t.Setenv("ZOTGO_CONFIG_DIR", t.TempDir())
	}
	strs := make([]string, len(ops))
	for i, op := range ops {
		strs[i] = string(op)
	}
	l := &lease{
		ID:       "lease_scoped",
		Created:  time.Now(),
		Expires:  expires,
		Scope:    leaseScope{Libraries: []string{"user:0"}, Operations: strs},
		WriteKey: "TEST-KEY",
	}
	if err := saveLease(l); err != nil {
		t.Fatalf("saveLease: %v", err)
	}
}

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	t.Setenv("XDG_CONFIG_HOME", dir)

	if k := loadLocalKey(); k != "" {
		t.Fatalf("fresh config has a key: %q", k)
	}
	if err := saveLocalKey("SECRET123"); err != nil {
		t.Fatalf("saveLocalKey: %v", err)
	}
	if k := loadLocalKey(); k != "SECRET123" {
		t.Errorf("loadLocalKey = %q, want SECRET123", k)
	}
	// Stored owner-only.
	path := filepath.Join(dir, "zotgo", "local-api-key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 600", perm)
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

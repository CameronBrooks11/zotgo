package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic_WritesContentAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bib")

	if err := writeFileAtomic(path, []byte("@book{a}\n")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "@book{a}\n" {
		t.Fatalf("content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Fatalf("mode = %v, want 0644", perm)
		}
	}
}

// The temp file must be renamed away, never left beside the target.
func TestWriteFileAtomic_LeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := writeFileAtomic(path, []byte("[]")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "out.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v, want only out.json", names)
	}
}

func TestWriteFileAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := writeFileAtomic(path, []byte("fresh")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "fresh" {
		t.Fatalf("content = %q, want fresh", got)
	}
}

// A failed write must not destroy the previous contents, and must not strand a
// temp file. An unwritable directory is the simplest way to force the failure.
func TestWriteFileAtomic_FailureLeavesTargetIntact(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not block writes here")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := writeFileAtomic(path, []byte("replacement")); err == nil {
		t.Fatal("writeFileAtomic succeeded in a read-only directory")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("content = %q, want the original to survive", got)
	}
}

//go:build !windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStageAttachmentFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := stageAttachmentFile(path, "paper.pdf", "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("staging a FIFO blocked")
	}
}

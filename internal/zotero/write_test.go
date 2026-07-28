package zotero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The client caches Zotero-Server-ID from ordinary reads so it can resend it on
// writes, which require it. A build without the header leaves the cache empty.
func TestServerID_CapturedFromReads(t *testing.T) {
	var serveHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if serveHeader {
			w.Header().Set("Zotero-Server-ID", "srv-xyz-1")
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if c.ServerID() != "" {
		t.Fatalf("fresh client ServerID = %q, want empty", c.ServerID())
	}

	// A build predating the write API sends no header: cache stays empty.
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if c.ServerID() != "" {
		t.Errorf("ServerID = %q, want empty when no header is sent", c.ServerID())
	}

	// A write-capable build sends it on every response; the client captures it.
	serveHeader = true
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if c.ServerID() != "srv-xyz-1" {
		t.Errorf("ServerID = %q, want srv-xyz-1", c.ServerID())
	}
}

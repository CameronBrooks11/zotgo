package zotero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeZotero stands in for a running Zotero. When localAPIEnabled is false, its
// protected Local API route replies 403 exactly as Zotero does when the
// httpServer.localAPI pref is off.
func fakeZotero(localAPIEnabled bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Zotero is running</body></html>"))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		if !localAPIEnabled {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Local API is not enabled"))
			return
		}
		w.Header().Set("Zotero-Schema-Version", "42")
		w.Header().Set("Zotero-API-Version", "3")
		w.Header().Set("Total-Results", "2203")
		_, _ = w.Write([]byte("[]"))
	})
	return httptest.NewServer(mux)
}

func TestCheckHealth_Ready(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()

	h := New(srv.URL).CheckHealth(context.Background())

	if !h.Ready() {
		t.Fatalf("expected Ready, got %+v", h)
	}
	if !h.ZoteroRunning || !h.LocalAPIEnabled {
		t.Errorf("running=%v enabled=%v, want both true", h.ZoteroRunning, h.LocalAPIEnabled)
	}
	if h.ZoteroVersion != "9.9.9" {
		t.Errorf("ZoteroVersion = %q, want 9.9.9", h.ZoteroVersion)
	}
	if h.SchemaVersion != "42" || h.APIVersion != "3" {
		t.Errorf("schema=%q api=%q, want 42/3", h.SchemaVersion, h.APIVersion)
	}
}

func TestCheckHealth_LocalAPIDisabled(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()

	h := New(srv.URL).CheckHealth(context.Background())

	if h.Ready() {
		t.Fatalf("expected not Ready, got %+v", h)
	}
	if !h.ZoteroRunning {
		t.Errorf("ZoteroRunning = false, want true (Zotero is up, only the API is off)")
	}
	if h.LocalAPIEnabled {
		t.Errorf("LocalAPIEnabled = true, want false")
	}
	if h.ZoteroVersion != "9.9.9" {
		t.Errorf("ZoteroVersion = %q, want 9.9.9 (from connector ping)", h.ZoteroVersion)
	}
}

func TestCheckHealth_ZoteroDown(t *testing.T) {
	// Start then immediately stop a server to get a definitely-refused address.
	srv := fakeZotero(true)
	url := srv.URL
	srv.Close()

	h := New(url).CheckHealth(context.Background())

	if h.ZoteroRunning {
		t.Fatalf("expected ZoteroRunning=false against a closed server, got %+v", h)
	}
	if h.Ready() {
		t.Errorf("expected not Ready when Zotero is down")
	}
}

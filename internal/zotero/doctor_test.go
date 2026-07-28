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

// A Zotero build with the local write API returns a Zotero-Server-ID header on
// every response; doctor derives write support from its presence.
func TestCheckHealth_LocalWriteAPIDetectedViaServerID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		_, _ = w.Write([]byte("Zotero is running"))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Zotero-Server-ID", "srv-abc-123")
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := New(srv.URL).CheckHealth(context.Background())
	if h.ServerID != "srv-abc-123" {
		t.Fatalf("ServerID = %q, want it captured from the header", h.ServerID)
	}
	if !h.Supports(CapabilityWrite) {
		t.Error("write should be supported when Zotero-Server-ID is present")
	}
}

// fakeWebAPI serves /keys/current with the given user grants, or a 403 when
// grant is empty (a rejected key).
func fakeWebAPI(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if body == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Zotero-API-Version", "3")
		_, _ = w.Write([]byte(body))
	}))
}

func capReason(h Health, name Capability) (bool, string) {
	for _, c := range h.Capabilities() {
		if c.Name == name {
			return c.Supported, c.Reason
		}
	}
	return false, "missing"
}

func TestCheckHealth_WebReadOnlyKeyIsReady(t *testing.T) {
	srv := fakeWebAPI(`{"userID":9,"access":{"user":{"library":true,"write":false}}}`)
	defer srv.Close()

	h := NewWeb(srv.URL, "k").CheckHealth(context.Background())

	if !h.Reachable || !h.KeyValid || h.WebUserID != 9 {
		t.Fatalf("reachable/keyValid/userID = %v/%v/%d, want true/true/9", h.Reachable, h.KeyValid, h.WebUserID)
	}
	if !h.Ready() {
		t.Fatal("read-only key with library access should be Ready")
	}
	if ok, _ := capReason(h, CapabilityRead); !ok {
		t.Error("read capability should be supported")
	}
	if ok, reason := capReason(h, CapabilityWrite); ok || reason == "" {
		t.Errorf("write = supported %v (%q), want unsupported with a reason", ok, reason)
	}
	if ok, reason := capReason(h, CapabilityConnectorIngest); ok || reason == "" {
		t.Errorf("connector-ingest = %v (%q), want unsupported with a local-only reason", ok, reason)
	}
}

func TestCheckHealth_WebWriteKeyReportsWrite(t *testing.T) {
	srv := fakeWebAPI(`{"userID":9,"access":{"user":{"library":true,"write":true}}}`)
	defer srv.Close()

	h := NewWeb(srv.URL, "k").CheckHealth(context.Background())
	if ok, _ := capReason(h, CapabilityWrite); !ok {
		t.Error("a key granting write should report write supported (the endpoint allows it)")
	}
}

func TestCheckHealth_WebInvalidKey(t *testing.T) {
	srv := fakeWebAPI("")
	defer srv.Close()

	h := NewWeb(srv.URL, "bad").CheckHealth(context.Background())
	if !h.Reachable {
		t.Error("a 403 still proves the endpoint is reachable")
	}
	if h.KeyValid || h.Ready() {
		t.Errorf("keyValid=%v ready=%v, want both false", h.KeyValid, h.Ready())
	}
	if ok, reason := capReason(h, CapabilityRead); ok || reason == "" {
		t.Errorf("read = %v (%q), want unsupported citing the key", ok, reason)
	}
}

func TestCheckHealth_WebUnreachable(t *testing.T) {
	srv := fakeWebAPI(`{"userID":9,"access":{}}`)
	url := srv.URL
	srv.Close()

	h := NewWeb(url, "k").CheckHealth(context.Background())
	if h.Reachable || h.Ready() {
		t.Errorf("reachable=%v ready=%v against a closed server, want both false", h.Reachable, h.Ready())
	}
	if _, reason := capReason(h, CapabilityRead); reason == "" {
		t.Error("unreachable read capability should carry a reason")
	}
}

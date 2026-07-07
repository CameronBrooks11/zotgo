package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeZotero(localAPIEnabled bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		_, _ = w.Write([]byte("Zotero is running"))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		if !localAPIEnabled {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Zotero-Schema-Version", "42")
		w.Header().Set("Zotero-API-Version", "3")
		_, _ = w.Write([]byte("[]"))
	})
	return httptest.NewServer(mux)
}

func TestRun_DoctorReady(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()

	var out, errBuf bytes.Buffer
	code := run([]string{"doctor", "--url", srv.URL}, &out, &errBuf)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Ready") {
		t.Errorf("output missing Ready:\n%s", out.String())
	}
}

func TestRun_DoctorLocalAPIDisabled(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()

	var out, errBuf bytes.Buffer
	code := run([]string{"doctor", "--url", srv.URL}, &out, &errBuf)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "Local API disabled") {
		t.Errorf("output missing disabled guidance:\n%s", out.String())
	}
}

func TestRun_DoctorZoteroDown(t *testing.T) {
	srv := fakeZotero(true)
	url := srv.URL
	srv.Close()

	var out, errBuf bytes.Buffer
	code := run([]string{"doctor", "--url", url}, &out, &errBuf)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Errorf("output missing not-running guidance:\n%s", out.String())
	}
}

func TestRun_Version(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"version"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(out.String(), "zot ") {
		t.Errorf("version output = %q", out.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errBuf); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "unknown command") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

func TestRun_NoArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run(nil, &out, &errBuf); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

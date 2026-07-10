package zotero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNew_DefaultsBaseURLAndClient(t *testing.T) {
	c := New("")
	if c.BaseURL() != DefaultBaseURL {
		t.Fatalf("BaseURL() = %q, want %q", c.BaseURL(), DefaultBaseURL)
	}
	if c.http == nil {
		t.Fatal("http client is nil")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	if got := New("http://x:1/").BaseURL(); got != "http://x:1" {
		t.Fatalf("BaseURL() = %q, want %q", got, "http://x:1")
	}
}

// WithHTTPClient must replace the transport actually used for requests.
func TestWithHTTPClient_IsUsed(t *testing.T) {
	var seen *http.Request
	custom := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     http.Header{},
				Request:    r,
			}, nil
		}),
	}

	c := New("http://zotero.invalid", WithHTTPClient(custom))
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if seen == nil {
		t.Fatal("custom http.Client was not used")
	}
	if got := seen.Header.Get("Zotero-API-Version"); got != "3" {
		t.Fatalf("Zotero-API-Version = %q, want 3", got)
	}
	if seen.URL.Host != "zotero.invalid" {
		t.Fatalf("host = %q, want zotero.invalid", seen.URL.Host)
	}
}

// A nil client must not clobber the working default.
func TestWithHTTPClient_NilIsIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithHTTPClient(nil))
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items with nil option: %v", err)
	}
}

func TestWithTimeout(t *testing.T) {
	c := New("", WithTimeout(90*time.Second))
	if c.http.Timeout != 90*time.Second {
		t.Fatalf("Timeout = %v, want 90s", c.http.Timeout)
	}
}

// WithTimeout must not mutate a caller-owned http.Client.
func TestWithTimeout_DoesNotMutateCallerClient(t *testing.T) {
	caller := &http.Client{Timeout: time.Second}
	_ = New("", WithHTTPClient(caller), WithTimeout(90*time.Second))
	if caller.Timeout != time.Second {
		t.Fatalf("caller's client was mutated: Timeout = %v, want 1s", caller.Timeout)
	}
}

package zotero

import (
	"context"
	"net/http"
	"testing"
)

// captureTransport records the last request it saw and returns an empty JSON
// array, so a single read call can be inspected without a live server.
func captureTransport(seen **http.Request) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			*seen = r
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
				Header:     http.Header{},
				Request:    r,
			}, nil
		}),
	}
}

func TestNewWeb_ProfileAndBaseURL(t *testing.T) {
	c := NewWeb("", "secret")
	if got := c.Profile().Kind; got != EndpointWeb {
		t.Fatalf("Profile().Kind = %q, want %q", got, EndpointWeb)
	}
	if got := c.BaseURL(); got != DefaultWebBaseURL {
		t.Fatalf("BaseURL() = %q, want %q", got, DefaultWebBaseURL)
	}
	if c.http.Timeout != DefaultWebTimeout {
		t.Fatalf("Timeout = %v, want %v", c.http.Timeout, DefaultWebTimeout)
	}
}

func TestNewWeb_TrimsTrailingSlash(t *testing.T) {
	if got := NewWeb("https://api.zotero.org/", "k").BaseURL(); got != "https://api.zotero.org" {
		t.Fatalf("BaseURL() = %q, want trimmed", got)
	}
}

// LibraryPrefix is the whole routing difference between the two endpoints: the
// Web API drops /api and addresses the user by a real numeric id, while the
// Local API keeps /api and uses the "0" sentinel.
func TestLibraryPrefix_ByEndpoint(t *testing.T) {
	local := LocalProfile("")
	web := WebProfile("")
	cases := []struct {
		name    string
		profile Profile
		lib     LibraryRef
		want    string
	}{
		{"local user", local, UserLibrary(), "/api/users/0"},
		{"local group", local, GroupLibrary(101, "G"), "/api/groups/101"},
		{"web user", web, LibraryRef{Kind: LibraryKindUser, ID: 12345}, "/users/12345"},
		{"web group", web, GroupLibrary(101, "G"), "/groups/101"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.profile.LibraryPrefix(tc.lib); got != tc.want {
				t.Fatalf("LibraryPrefix = %q, want %q", got, tc.want)
			}
		})
	}
}

// A web request must carry the API key and hit the /api-less web route.
func TestNewWeb_SendsAPIKeyAndHitsWebRoute(t *testing.T) {
	var seen *http.Request
	c := NewWeb("https://api.zotero.org", "s3cret", WithHTTPClient(captureTransport(&seen)))

	lib := LibraryRef{Kind: LibraryKindUser, ID: 12345}
	if _, _, err := c.Items(context.Background(), lib, ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if seen == nil {
		t.Fatal("no request captured")
	}
	if got := seen.Header.Get("Zotero-API-Key"); got != "s3cret" {
		t.Fatalf("Zotero-API-Key = %q, want s3cret", got)
	}
	if got := seen.Header.Get("Zotero-API-Version"); got != "3" {
		t.Fatalf("Zotero-API-Version = %q, want 3", got)
	}
	if got := seen.URL.Path; got != "/users/12345/items" {
		t.Fatalf("path = %q, want /users/12345/items", got)
	}
	if seen.URL.Host != "api.zotero.org" {
		t.Fatalf("host = %q, want api.zotero.org", seen.URL.Host)
	}
}

// The local endpoint must never send an API key: it has no credential to send,
// and a stray header would be a leak waiting to happen.
func TestNew_LocalSendsNoAPIKey(t *testing.T) {
	var seen *http.Request
	c := New("http://localhost:23119", WithHTTPClient(captureTransport(&seen)))
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if got := seen.Header.Get("Zotero-API-Key"); got != "" {
		t.Fatalf("local request carried Zotero-API-Key = %q, want none", got)
	}
	if got := seen.URL.Path; got != "/api/users/0/items" {
		t.Fatalf("path = %q, want /api/users/0/items", got)
	}
}

// Package zotero is zotgo's client for a running Zotero 7+ desktop app.
//
// It speaks only Zotero's own HTTP contracts — today the Local API (/api/*),
// which is read-only — and never opens zotero.sqlite. The package depends on
// the standard library alone.
//
// This is an internal package, not a published SDK: it cannot be imported from
// outside this module, and its API is free to change with zotgo's needs.
package zotero

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is Zotero's default local HTTP server address.
const DefaultBaseURL = "http://localhost:23119"

// DefaultWebBaseURL is the hosted Web API's address.
const DefaultWebBaseURL = "https://api.zotero.org"

// Client talks to a running Zotero over its Local API and Connector API, or to
// the hosted Web API — which of the two is fixed by its Profile at construction.
//
// A Client is safe for concurrent use. It performs no I/O until a method is
// called; construction never fails.
type Client struct {
	profile Profile
	auth    Authenticator
	http    *http.Client
}

// DefaultTimeout bounds a single Local API round-trip, body included.
const DefaultTimeout = 5 * time.Second

// DefaultWebTimeout bounds a single Web API round-trip. It is looser than the
// local default because the request crosses the public internet rather than a
// loopback socket.
const DefaultWebTimeout = 30 * time.Second

// An Option configures a Client at construction.
type Option func(*Client)

// WithHTTPClient makes the Client issue its requests through h, which carries
// the transport, redirect policy, and timeout. Use it to supply a custom
// transport for retries, tracing, or tests. A nil h is ignored.
//
// The Client copies h, so later changes to the caller's value — including those
// made by WithTimeout — do not affect it.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h == nil {
			return
		}
		cp := *h
		c.http = &cp
	}
}

// WithTimeout bounds each round-trip. It overrides any timeout carried by a
// client passed to WithHTTPClient, so pass it afterwards to take effect.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// New returns a Client for a running local Zotero at baseURL, or DefaultBaseURL
// when baseURL is empty. The local endpoint needs no credential. Options are
// applied in order.
func New(baseURL string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return newClient(LocalProfile(baseURL), noAuth{}, DefaultTimeout, opts...)
}

// NewWeb returns a Client for the hosted Web API at baseURL, or DefaultWebBaseURL
// when baseURL is empty, authenticated with apiKey. Options are applied in order.
func NewWeb(baseURL, apiKey string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultWebBaseURL
	}
	return newClient(WebProfile(baseURL), apiKeyAuth{key: apiKey}, DefaultWebTimeout, opts...)
}

func newClient(profile Profile, auth Authenticator, timeout time.Duration, opts ...Option) *Client {
	profile.BaseURL = strings.TrimRight(profile.BaseURL, "/")
	c := &Client{
		profile: profile,
		auth:    auth,
		http:    &http.Client{Timeout: timeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL reports the address the client targets.
func (c *Client) BaseURL() string { return c.profile.BaseURL }

// get issues a GET against the endpoint's base URL + path, carrying the API
// version and whatever credential the endpoint requires. The caller owns the
// response body and must close it.
func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.profile.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	c.auth.authorize(req)
	return c.http.Do(req)
}

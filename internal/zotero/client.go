// Package zotero is zotgo's SDK for a running Zotero 7+ desktop app.
//
// It speaks only Zotero's own HTTP contracts — the Local API (/api/*) for
// reads and the Connector API (/connector/*) for writes — and never opens
// zotero.sqlite. The package depends on the standard library alone.
package zotero

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is Zotero's default local HTTP server address.
const DefaultBaseURL = "http://localhost:23119"

// Client talks to a running Zotero over its Local API and Connector API.
//
// A Client is safe for concurrent use. It performs no I/O until a method is
// called; construction never fails.
type Client struct {
	baseURL string
	http    *http.Client
}

// DefaultTimeout bounds a single Local API round-trip, body included.
const DefaultTimeout = 5 * time.Second

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

// New returns a Client targeting baseURL, or DefaultBaseURL when baseURL is
// empty. Options are applied in order.
func New(baseURL string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	c := &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL reports the address the client targets.
func (c *Client) BaseURL() string { return c.baseURL }

// get issues a GET against baseURL+path. The caller owns the response body and
// must close it.
func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	return c.http.Do(req)
}

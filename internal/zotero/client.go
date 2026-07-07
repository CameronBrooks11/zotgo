// Package zotero is zotgo's SDK for a running Zotero 7+ desktop app.
//
// It speaks only Zotero's own HTTP contracts — the Local API (/api/*) for
// reads and the Connector API (/connector/*) for writes — and never opens
// zotero.sqlite. The package depends on the standard library alone.
package zotero

import (
	"context"
	"net/http"
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

// New returns a Client targeting baseURL, or DefaultBaseURL when baseURL is
// empty.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
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
	return c.http.Do(req)
}

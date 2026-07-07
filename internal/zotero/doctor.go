package zotero

import (
	"context"
	"net/http"
)

// Health is a snapshot of whether zotgo can talk to Zotero and which of its two
// HTTP surfaces are usable.
type Health struct {
	// BaseURL is the address that was probed.
	BaseURL string
	// ZoteroRunning is true when the Connector HTTP server accepted a
	// connection (Zotero 7+ is running).
	ZoteroRunning bool
	// ZoteroVersion is the X-Zotero-Version reported by the running app.
	ZoteroVersion string
	// LocalAPIEnabled is true when the Local API (/api/*) serves protected
	// routes rather than 403 "Local API is not enabled".
	LocalAPIEnabled bool
	// SchemaVersion and APIVersion are reported by the Local API when enabled.
	SchemaVersion string
	APIVersion    string
}

// Ready reports whether both surfaces zotgo depends on for reads are usable:
// Zotero is running and its Local API is enabled.
func (h Health) Ready() bool {
	return h.ZoteroRunning && h.LocalAPIEnabled
}

// CheckHealth probes Zotero and returns a Health snapshot. It never returns an
// error: an unreachable or misconfigured Zotero is a normal, renderable result,
// not a failure of the probe.
//
// Two requests are made against endpoints that are always safe to hit:
//
//   - GET /connector/ping — never gated; establishes liveness and version.
//   - GET /api/users/0/items?limit=1 — a protected Local API route that returns
//     200 when the API is enabled and 403 when the pref is off.
func (c *Client) CheckHealth(ctx context.Context) Health {
	h := Health{BaseURL: c.baseURL}

	// Liveness + version. The connector ping is never gated by any pref, so a
	// refused connection is the authoritative "Zotero is not running" signal.
	resp, err := c.get(ctx, "/connector/ping")
	if err != nil {
		return h
	}
	h.ZoteroRunning = true
	h.ZoteroVersion = resp.Header.Get("X-Zotero-Version")
	resp.Body.Close()

	// Local API enabled? A protected route 403s when httpServer.localAPI is off.
	resp, err = c.get(ctx, "/api/users/0/items?limit=1")
	if err != nil {
		return h
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		h.LocalAPIEnabled = true
		h.SchemaVersion = resp.Header.Get("Zotero-Schema-Version")
		h.APIVersion = resp.Header.Get("Zotero-API-Version")
	case http.StatusForbidden:
		h.LocalAPIEnabled = false
	}
	if h.ZoteroVersion == "" {
		h.ZoteroVersion = resp.Header.Get("X-Zotero-Version")
	}
	return h
}

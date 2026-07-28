package zotero

import (
	"context"
	"encoding/json"
	"net/http"
)

// Health is a snapshot of whether zotgo can reach the active endpoint and what
// it may do there. Some fields describe only one endpoint kind; which apply is
// fixed by Endpoint.Kind.
type Health struct {
	// Endpoint identifies what was probed: its kind and address.
	Endpoint Profile
	// Reachable is the liveness signal both endpoints share: the local Connector
	// accepted a connection, or the Web API answered at all (even to reject a key).
	Reachable bool

	// Local endpoint only.
	//
	// ZoteroRunning is true when the Connector HTTP server accepted a connection
	// (Zotero 7+ is running). ZoteroVersion is its reported version.
	// LocalAPIEnabled is true when the Local API serves protected routes rather
	// than 403 "Local API is not enabled".
	ZoteroRunning   bool
	ZoteroVersion   string
	LocalAPIEnabled bool

	// Web endpoint only.
	//
	// KeyValid is true when /keys/current accepted the API key and it resolves to
	// a user. WebUserID is that user's id.
	KeyValid  bool
	WebUserID int64
	// webKey carries the key's access grants for capability derivation. Kept
	// unexported: it is a probe detail, and the credential's grants are not part
	// of the machine-output contract. Nil for the local endpoint.
	webKey *WebKey

	// SchemaVersion and APIVersion are reported by either endpoint on a
	// successful protected read.
	SchemaVersion string
	APIVersion    string
}

// Ready reports whether the active endpoint is usable for reads: locally, that
// Zotero is running and its Local API is enabled; on the Web API, that the key
// is valid and grants some library read access.
func (h Health) Ready() bool {
	if h.Endpoint.Kind == EndpointWeb {
		return h.KeyValid && h.Supports(CapabilityRead)
	}
	return h.ZoteroRunning && h.LocalAPIEnabled
}

// CheckHealth probes the active endpoint and returns a Health snapshot. It never
// returns an error: an unreachable or misconfigured endpoint is a normal,
// renderable result, not a failure of the probe.
func (c *Client) CheckHealth(ctx context.Context) Health {
	if c.profile.Kind == EndpointWeb {
		return c.checkWeb(ctx)
	}
	return c.checkLocal(ctx)
}

// checkLocal probes a running desktop Zotero with two always-safe requests:
//
//   - GET /connector/ping — never gated; establishes liveness and version.
//   - GET /api/users/0/items?limit=1 — a protected Local API route that returns
//     200 when the API is enabled and 403 when the pref is off.
func (c *Client) checkLocal(ctx context.Context) Health {
	h := Health{Endpoint: c.profile}

	// Liveness + version. The connector ping is never gated by any pref, so a
	// refused connection is the authoritative "Zotero is not running" signal.
	resp, err := c.get(ctx, "/connector/ping")
	if err != nil {
		return h
	}
	h.ZoteroRunning = true
	h.Reachable = true
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

// checkWeb probes the Web API with a single GET /keys/current: the server is
// always up, so the meaningful question is whether the key is accepted. A
// response — even a 403 — proves reachability; a 200 that names a user proves
// the key, and its body carries the grants that drive capability reporting.
func (c *Client) checkWeb(ctx context.Context) Health {
	h := Health{Endpoint: c.profile}

	resp, err := c.get(ctx, "/keys/current")
	if err != nil {
		return h
	}
	defer resp.Body.Close()
	h.Reachable = true
	h.SchemaVersion = resp.Header.Get("Zotero-Schema-Version")
	h.APIVersion = resp.Header.Get("Zotero-API-Version")

	if resp.StatusCode == http.StatusOK {
		var k WebKey
		if err := json.NewDecoder(resp.Body).Decode(&k); err == nil && k.UserID != 0 {
			h.KeyValid = true
			h.WebUserID = k.UserID
			h.webKey = &k
		}
	}
	return h
}

package zotero

import "fmt"

// EndpointKind distinguishes the endpoints zotgo can be pointed at.
//
// The durable axis is local vs remote, not read vs write: Zotero's Web API
// already offers remote CRUD, and its Local API is read-only only for now. Each
// endpoint is its own version and concurrency domain, so an operation is never
// silently moved between them.
type EndpointKind string

const (
	// EndpointLocal is a Zotero 7+ desktop app's HTTP server on this machine.
	EndpointLocal EndpointKind = "local"
	// EndpointWeb is the hosted Web API at api.zotero.org, reached with an API
	// key. Its transport is implemented; the CLI wiring lands with v0.5.
	EndpointWeb EndpointKind = "web"
)

// Profile is the printable identity of the endpoint a Client talks to: its kind
// and address, nothing more.
//
// It deliberately carries no credential. The Web API key lives on the Client's
// Authenticator, not here, so a Profile is always safe to log or embed in
// machine output without leaking a secret. It also carries no server identity:
// Zotero-Server-ID was coined upstream but appears in no shipped Zotero, so
// encoding it would be a guess (see working/plan.md §3).
type Profile struct {
	Kind    EndpointKind
	BaseURL string
}

// LocalProfile describes a running Zotero on this machine.
func LocalProfile(baseURL string) Profile {
	return Profile{Kind: EndpointLocal, BaseURL: baseURL}
}

// WebProfile describes the hosted Web API at baseURL (api.zotero.org).
func WebProfile(baseURL string) Profile {
	return Profile{Kind: EndpointWeb, BaseURL: baseURL}
}

// LibraryPrefix is the API path prefix for a library's resources on this
// endpoint. The Local API namespaces everything under /api and addresses the
// logged-in user as the "0" sentinel; the Web API omits /api and uses the real
// numeric user id carried by the LibraryRef.
func (p Profile) LibraryPrefix(lib LibraryRef) string {
	root := "/api"
	if p.Kind == EndpointWeb {
		root = ""
	}
	if lib.Kind == LibraryKindGroup {
		return fmt.Sprintf("%s/groups/%d", root, lib.ID)
	}
	return fmt.Sprintf("%s/users/%d", root, lib.ID)
}

// Profile reports the endpoint this Client targets.
func (c *Client) Profile() Profile { return c.profile }

// Capability is something an endpoint can do. It describes the *endpoint*, not
// what zotgo has implemented against it: doctor answers "what does this Zotero
// allow?", and a capability may be available before a command uses it.
type Capability string

const (
	// CapabilityRead is reading items, collections, and tags.
	CapabilityRead Capability = "read"
	// CapabilityWrite is creating, updating, or deleting objects through the
	// official API write contract.
	CapabilityWrite Capability = "write"
	// CapabilityConnectorIngest is app-mediated ingestion over /connector/*:
	// PDF recognition, file import, snapshots.
	CapabilityConnectorIngest Capability = "connector-ingest"
	// CapabilityLocalFileAccess is resolving an attachment to a local file path.
	CapabilityLocalFileAccess Capability = "local-file-access"
)

// CapabilityStatus reports whether one capability is available, and why not when
// it is missing. A missing capability without a reason is a bug: the user is
// left with nothing to act on.
type CapabilityStatus struct {
	Name      Capability
	Supported bool
	// Reason explains an unsupported capability, in terms the user can act on
	// or at least understand. Empty when Supported.
	Reason string
}

// localAPIUnavailable explains why the Local API cannot serve a request, or
// returns "" when it can.
func (h Health) localAPIUnavailable() string {
	switch {
	case !h.ZoteroRunning:
		return "Zotero is not running"
	case !h.LocalAPIEnabled:
		return "Zotero's Local API is disabled"
	default:
		return ""
	}
}

// Capabilities reports what the probed endpoint supports, in a stable order.
//
// Capabilities are derived from the probe rather than discovered by exercising
// them: zotgo will not issue a speculative write to find out whether writes are
// allowed. The Web endpoint can report writes honestly because the key's own
// grants say so; the local endpoint cannot, and says why.
func (h Health) Capabilities() []CapabilityStatus {
	if h.Endpoint.Kind == EndpointWeb {
		return h.webCapabilities()
	}
	return h.localCapabilities()
}

func (h Health) localCapabilities() []CapabilityStatus {
	blocked := h.localAPIUnavailable()

	caps := []CapabilityStatus{
		{Name: CapabilityRead, Supported: blocked == "", Reason: blocked},
		h.localWriteCapability(blocked),
		{Name: CapabilityConnectorIngest, Supported: h.ZoteroRunning},
		{Name: CapabilityLocalFileAccess, Supported: blocked == "", Reason: blocked},
	}
	if !h.ZoteroRunning {
		caps[2].Reason = "Zotero is not running"
	}
	return caps
}

// localWriteCapability is now probe-derived: the Local API returns a
// Zotero-Server-ID header on every response once it has the write endpoints
// (zotero/zotero#5015). Its presence means this build supports writes; its
// absence means the build predates them. When the API cannot even be read, the
// blocking reason takes precedence — we cannot tell either way.
func (h Health) localWriteCapability(blocked string) CapabilityStatus {
	switch {
	case blocked != "":
		return CapabilityStatus{Name: CapabilityWrite, Reason: blocked}
	case h.ServerID == "":
		return CapabilityStatus{
			Name:   CapabilityWrite,
			Reason: "this Zotero build has no local write API (added upstream in zotero/zotero#5015; update Zotero once a release ships it)",
		}
	default:
		return CapabilityStatus{Name: CapabilityWrite, Supported: true}
	}
}

// webCapabilities reads reads and writes from the key's grants — the Web API's
// own statement of what the key may do. Connector ingestion and local-file
// access are inherently local-desktop features with no Web API equivalent.
func (h Health) webCapabilities() []CapabilityStatus {
	var readReason, writeReason string
	var canRead, canWrite bool
	switch {
	case !h.Reachable:
		readReason = "api.zotero.org is unreachable"
		writeReason = readReason
	case !h.KeyValid:
		readReason = "the API key is missing, revoked, or wrong"
		writeReason = readReason
	default:
		// A valid key always carries its grants; guard anyway so a hand-built
		// Health cannot panic here.
		if h.webKey != nil {
			canRead = h.webKey.grantsRead()
			canWrite = h.webKey.grantsWrite()
		}
		if !canRead {
			readReason = "the API key grants no library read access"
		}
		if !canWrite {
			writeReason = "the API key is read-only"
		}
	}
	return []CapabilityStatus{
		{Name: CapabilityRead, Supported: canRead, Reason: readReason},
		{Name: CapabilityWrite, Supported: canWrite, Reason: writeReason},
		{Name: CapabilityConnectorIngest, Supported: false, Reason: "Connector ingestion needs a local Zotero; the Web API has no equivalent"},
		{Name: CapabilityLocalFileAccess, Supported: false, Reason: "resolving attachments to local files needs a local Zotero"},
	}
}

// Supports reports whether the endpoint offers a capability.
func (h Health) Supports(c Capability) bool {
	for _, status := range h.Capabilities() {
		if status.Name == c {
			return status.Supported
		}
	}
	return false
}

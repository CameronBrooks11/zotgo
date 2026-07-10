package zotero

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
	// EndpointWeb (api.zotero.org) is not implemented; it lands with the
	// authenticated Web profile. Named here only so that switches over
	// EndpointKind read as deliberate rather than accidental.
	EndpointWeb EndpointKind = "web"
)

// Profile is the identity of the endpoint a Client talks to.
//
// It carries no authentication strategy and no server identity yet: the local
// endpoint needs neither, and inventing either before the Web profile exists
// would encode guesses rather than contracts.
type Profile struct {
	Kind    EndpointKind
	BaseURL string
}

// LocalProfile describes a running Zotero on this machine.
func LocalProfile(baseURL string) Profile {
	return Profile{Kind: EndpointLocal, BaseURL: baseURL}
}

// Profile reports the endpoint this Client targets.
func (c *Client) Profile() Profile { return LocalProfile(c.baseURL) }

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
// allowed.
func (h Health) Capabilities() []CapabilityStatus {
	blocked := h.localAPIUnavailable()

	caps := []CapabilityStatus{
		{Name: CapabilityRead, Supported: blocked == "", Reason: blocked},
		{
			Name:      CapabilityWrite,
			Supported: false,
			// Not a probe result: every Local API endpoint is a GET today.
			// Upstream tracks local writes as zotero/zotero#5015.
			Reason: "Zotero's Local API exposes no write endpoints (upstream: zotero/zotero#5015)",
		},
		{Name: CapabilityConnectorIngest, Supported: h.ZoteroRunning},
		{Name: CapabilityLocalFileAccess, Supported: blocked == "", Reason: blocked},
	}
	if !h.ZoteroRunning {
		caps[2].Reason = "Zotero is not running"
	}
	return caps
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

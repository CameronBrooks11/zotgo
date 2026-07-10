package zotero

import (
	"strings"
	"testing"
)

func TestLocalProfile(t *testing.T) {
	p := LocalProfile("http://localhost:23119")
	if p.Kind != EndpointLocal {
		t.Errorf("Kind = %q, want local", p.Kind)
	}
	if p.BaseURL != "http://localhost:23119" {
		t.Errorf("BaseURL = %q", p.BaseURL)
	}
}

// The client's profile must reflect the URL it actually targets, defaults and
// trailing-slash trimming included.
func TestClientProfile(t *testing.T) {
	if got := New("").Profile(); got != LocalProfile(DefaultBaseURL) {
		t.Errorf("New(\"\").Profile() = %+v", got)
	}
	if got := New("http://x:1/").Profile().BaseURL; got != "http://x:1" {
		t.Errorf("BaseURL = %q, want trimmed", got)
	}
}

func capByName(t *testing.T, h Health, name Capability) CapabilityStatus {
	t.Helper()
	for _, c := range h.Capabilities() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("capability %q not reported", name)
	return CapabilityStatus{}
}

func TestCapabilities_ReadyEndpoint(t *testing.T) {
	h := Health{Endpoint: LocalProfile("http://x"), ZoteroRunning: true, LocalAPIEnabled: true}

	if !h.Supports(CapabilityRead) {
		t.Error("read should be supported on a ready endpoint")
	}
	if !h.Supports(CapabilityConnectorIngest) {
		t.Error("connector ingestion should be supported when Zotero runs")
	}
	if !h.Supports(CapabilityLocalFileAccess) {
		t.Error("local file access should be supported on a ready endpoint")
	}
	// The whole point of modelling this: local writes do not exist yet.
	if h.Supports(CapabilityWrite) {
		t.Error("write must never be advertised on a local endpoint today")
	}
}

// Writes are unsupported regardless of how healthy the endpoint is, and the
// reason must point at the upstream issue rather than blame the user's setup.
func TestCapabilities_WriteIsAlwaysUnsupportedLocally(t *testing.T) {
	for _, h := range []Health{
		{ZoteroRunning: true, LocalAPIEnabled: true},
		{ZoteroRunning: true, LocalAPIEnabled: false},
		{ZoteroRunning: false},
	} {
		w := capByName(t, h, CapabilityWrite)
		if w.Supported {
			t.Fatalf("write supported for %+v", h)
		}
		if !strings.Contains(w.Reason, "5015") {
			t.Errorf("write reason should cite the upstream issue, got %q", w.Reason)
		}
	}
}

func TestCapabilities_ZoteroNotRunning(t *testing.T) {
	h := Health{Endpoint: LocalProfile("http://x")}

	for _, c := range h.Capabilities() {
		if c.Supported {
			t.Errorf("%q supported while Zotero is down", c.Name)
		}
	}
	if r := capByName(t, h, CapabilityRead).Reason; !strings.Contains(r, "not running") {
		t.Errorf("read reason = %q", r)
	}
	if r := capByName(t, h, CapabilityConnectorIngest).Reason; !strings.Contains(r, "not running") {
		t.Errorf("connector reason = %q", r)
	}
}

// Zotero is up but the Local API pref is off: ingestion still works, reads do not.
func TestCapabilities_LocalAPIDisabled(t *testing.T) {
	h := Health{Endpoint: LocalProfile("http://x"), ZoteroRunning: true}

	if h.Supports(CapabilityRead) {
		t.Error("read must not be supported when the Local API is disabled")
	}
	if !h.Supports(CapabilityConnectorIngest) {
		t.Error("the connector server is independent of the Local API pref")
	}
	if r := capByName(t, h, CapabilityRead).Reason; !strings.Contains(r, "Local API is disabled") {
		t.Errorf("read reason = %q", r)
	}
}

// A capability the user cannot act on is a capability we failed to explain.
func TestCapabilities_EveryUnsupportedOneHasAReason(t *testing.T) {
	for _, h := range []Health{
		{ZoteroRunning: true, LocalAPIEnabled: true},
		{ZoteroRunning: true},
		{},
	} {
		for _, c := range h.Capabilities() {
			if !c.Supported && c.Reason == "" {
				t.Errorf("%+v: capability %q unsupported with no reason", h, c.Name)
			}
			if c.Supported && c.Reason != "" {
				t.Errorf("%+v: capability %q supported but carries reason %q", h, c.Name, c.Reason)
			}
		}
	}
}

// Order is part of the contract: doctor and --json both render this list.
func TestCapabilities_StableOrder(t *testing.T) {
	want := []Capability{
		CapabilityRead, CapabilityWrite, CapabilityConnectorIngest, CapabilityLocalFileAccess,
	}
	got := Health{ZoteroRunning: true, LocalAPIEnabled: true}.Capabilities()
	if len(got) != len(want) {
		t.Fatalf("got %d capabilities, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("position %d = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestSupports_UnknownCapability(t *testing.T) {
	h := Health{ZoteroRunning: true, LocalAPIEnabled: true}
	if h.Supports(Capability("teleportation")) {
		t.Error("unknown capabilities must not be supported")
	}
}

// Ready means reads work; it must stay consistent with the read capability.
func TestReadyAgreesWithReadCapability(t *testing.T) {
	for _, h := range []Health{
		{ZoteroRunning: true, LocalAPIEnabled: true},
		{ZoteroRunning: true},
		{},
	} {
		if h.Ready() != h.Supports(CapabilityRead) {
			t.Errorf("%+v: Ready()=%v but read capability=%v", h, h.Ready(), h.Supports(CapabilityRead))
		}
	}
}

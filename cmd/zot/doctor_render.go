package main

import (
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// renderHealth writes a human-readable doctor report: what was probed, what the
// endpoint can do, and — for the states that block zotgo — how to fix it. The
// status lines and guidance are endpoint-specific; the capability list is not.
func renderHealth(w io.Writer, h zotero.Health) {
	fmt.Fprintf(w, "zot doctor — checking the %s endpoint at %s\n\n", h.Endpoint.Kind, h.Endpoint.BaseURL)

	web := h.Endpoint.Kind == zotero.EndpointWeb
	if web {
		renderWebStatus(w, h)
	} else {
		renderLocalStatus(w, h)
	}

	renderCapabilities(w, h.Capabilities())

	if web {
		renderWebGuidance(w, h)
	} else {
		renderLocalGuidance(w, h)
	}
}

func renderLocalStatus(w io.Writer, h zotero.Health) {
	if !h.ZoteroRunning {
		fmt.Fprintf(w, "  %s Zotero not running\n", cross)
		return
	}
	fmt.Fprintf(w, "  %s Zotero running%s\n", check, versionSuffix(h.ZoteroVersion))
	if h.LocalAPIEnabled {
		fmt.Fprintf(w, "  %s Local API enabled%s\n", check, schemaSuffix(h.SchemaVersion, h.APIVersion))
	} else {
		fmt.Fprintf(w, "  %s Local API disabled\n", cross)
	}
}

func renderLocalGuidance(w io.Writer, h zotero.Health) {
	switch {
	case !h.ZoteroRunning:
		fmt.Fprint(w, "\nStart the Zotero 7+ desktop app, then re-run `zot doctor`.\n")
		fmt.Fprint(w, "zotgo talks to a running Zotero over HTTP; it never reads your database directly.\n")
	case !h.LocalAPIEnabled:
		fmt.Fprint(w, "\nzotgo reads your library through Zotero's Local API, which is off by default.\n")
		fmt.Fprint(w, "To enable it:\n")
		fmt.Fprint(w, "  1. Zotero → Settings → Advanced → General\n")
		fmt.Fprint(w, "  2. Check \"Allow other applications on this computer to communicate with Zotero\"\n")
		fmt.Fprint(w, "  3. Re-run `zot doctor`\n")
	default:
		fmt.Fprint(w, "\nReady. zotgo can read your library.\n")
	}
}

func renderWebStatus(w io.Writer, h zotero.Health) {
	if !h.Reachable {
		fmt.Fprintf(w, "  %s api.zotero.org unreachable\n", cross)
		return
	}
	fmt.Fprintf(w, "  %s Web API reachable%s\n", check, schemaSuffix(h.SchemaVersion, h.APIVersion))
	if h.KeyValid {
		fmt.Fprintf(w, "  %s API key accepted (user %d)\n", check, h.WebUserID)
	} else {
		fmt.Fprintf(w, "  %s API key rejected\n", cross)
	}
}

func renderWebGuidance(w io.Writer, h zotero.Health) {
	switch {
	case !h.Reachable:
		fmt.Fprint(w, "\nCould not reach api.zotero.org. Check your network connection, then re-run.\n")
	case !h.KeyValid:
		fmt.Fprint(w, "\nzotgo authenticates to the Web API with a personal API key in ZOTGO_API_KEY.\n")
		fmt.Fprint(w, "Create one at https://www.zotero.org/settings/keys, then:\n")
		fmt.Fprint(w, "  export ZOTGO_API_KEY=…\n")
		fmt.Fprint(w, "  zot doctor --web\n")
	default:
		fmt.Fprint(w, "\nReady. zotgo can read your library over the Web API.\n")
	}
}

// renderCapabilities lists what the endpoint offers. Unsupported entries carry
// their reason, so an absent capability is never a bare cross.
//
// Reasons are aligned by explicit padding rather than a tabwriter: a supported
// line has no reason cell, and a tabwriter would leave it out of the column
// width and misalign the rest — or pad it with trailing whitespace.
func renderCapabilities(w io.Writer, caps []zotero.CapabilityStatus) {
	width := 0
	for _, c := range caps {
		if !c.Supported && len(c.Name) > width {
			width = len(c.Name)
		}
	}

	fmt.Fprint(w, "\nCapabilities:\n")
	for _, c := range caps {
		if c.Supported {
			fmt.Fprintf(w, "  %s %s\n", check, c.Name)
			continue
		}
		fmt.Fprintf(w, "  %s %-*s  %s\n", cross, width, c.Name, c.Reason)
	}
}

const (
	check = "✓"
	cross = "✗"
)

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return fmt.Sprintf("  (v%s)", v)
}

func schemaSuffix(schemaVer, apiVer string) string {
	switch {
	case schemaVer != "" && apiVer != "":
		return fmt.Sprintf("  (schema %s, API v%s)", schemaVer, apiVer)
	case apiVer != "":
		return fmt.Sprintf("  (API v%s)", apiVer)
	default:
		return ""
	}
}

package main

import (
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// renderHealth writes a human-readable doctor report: what was probed, what the
// endpoint can do, and — for the two states that block zotgo — how to fix it.
func renderHealth(w io.Writer, h zotero.Health) {
	fmt.Fprintf(w, "zot doctor — checking the %s endpoint at %s\n\n", h.Endpoint.Kind, h.Endpoint.BaseURL)

	if !h.ZoteroRunning {
		fmt.Fprintf(w, "  %s Zotero not running\n", cross)
	} else {
		fmt.Fprintf(w, "  %s Zotero running%s\n", check, versionSuffix(h.ZoteroVersion))
		if h.LocalAPIEnabled {
			fmt.Fprintf(w, "  %s Local API enabled%s\n", check, schemaSuffix(h.SchemaVersion, h.APIVersion))
		} else {
			fmt.Fprintf(w, "  %s Local API disabled\n", cross)
		}
	}

	renderCapabilities(w, h.Capabilities())

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

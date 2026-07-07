package main

import (
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// renderHealth writes a human-readable doctor report, including copy-paste fixes
// for the two states that block zotgo: Zotero not running, and the Local API
// being disabled.
func renderHealth(w io.Writer, h zotero.Health) {
	fmt.Fprintf(w, "zot doctor — checking Zotero at %s\n\n", h.BaseURL)

	if !h.ZoteroRunning {
		fmt.Fprintf(w, "  %s Zotero not running\n\n", cross)
		fmt.Fprint(w, "Start the Zotero 7+ desktop app, then re-run `zot doctor`.\n")
		fmt.Fprint(w, "zotgo talks to a running Zotero over HTTP; it never reads your database directly.\n")
		return
	}
	fmt.Fprintf(w, "  %s Zotero running%s\n", check, versionSuffix(h.ZoteroVersion))

	if !h.LocalAPIEnabled {
		fmt.Fprintf(w, "  %s Local API disabled\n\n", cross)
		fmt.Fprint(w, "zotgo reads your library through Zotero's Local API, which is off by default.\n")
		fmt.Fprint(w, "To enable it:\n")
		fmt.Fprint(w, "  1. Zotero → Settings → Advanced → General\n")
		fmt.Fprint(w, "  2. Check \"Allow other applications on this computer to communicate with Zotero\"\n")
		fmt.Fprint(w, "  3. Re-run `zot doctor`\n")
		return
	}
	fmt.Fprintf(w, "  %s Local API enabled%s\n\n", check, schemaSuffix(h.SchemaVersion, h.APIVersion))
	fmt.Fprint(w, "Ready. zotgo can read your library.\n")
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

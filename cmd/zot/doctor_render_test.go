package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func renderToString(h zotero.Health) string {
	var buf bytes.Buffer
	renderHealth(&buf, h, nil)
	return buf.String()
}

// capabilityLines returns the rendered lines of the Capabilities section.
func capabilityLines(t *testing.T, out string) []string {
	t.Helper()
	_, after, found := strings.Cut(out, "Capabilities:\n")
	if !found {
		t.Fatalf("no Capabilities section:\n%s", out)
	}
	var lines []string
	for _, line := range strings.Split(after, "\n") {
		if !strings.HasPrefix(line, "  ") {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		t.Fatalf("Capabilities section is empty:\n%s", out)
	}
	return lines
}

// Trailing whitespace is invisible in review and shows up in every diff of a
// golden file or a user's terminal copy-paste.
func TestRenderHealth_NoTrailingWhitespace(t *testing.T) {
	for _, h := range []zotero.Health{
		{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true, LocalAPIEnabled: true},
		{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true},
		{Endpoint: zotero.LocalProfile("http://x")},
		{Endpoint: zotero.WebProfile("https://api.zotero.org"), Reachable: true, KeyValid: true, WebUserID: 12345},
		{Endpoint: zotero.WebProfile("https://api.zotero.org"), Reachable: true},
		{Endpoint: zotero.WebProfile("https://api.zotero.org")},
	} {
		for i, line := range strings.Split(renderToString(h), "\n") {
			if line != strings.TrimRight(line, " \t") {
				t.Errorf("%+v: line %d has trailing whitespace: %q", h, i, line)
			}
		}
	}
}

// Reasons must line up, or the section is harder to scan than a plain list.
func TestRenderHealth_ReasonsAreAligned(t *testing.T) {
	h := zotero.Health{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true}

	var columns []int
	for _, line := range capabilityLines(t, renderToString(h)) {
		if !strings.Contains(line, "✗") {
			continue
		}
		// The reason begins after the capability name and its padding.
		idx := strings.Index(line, "Zotero")
		if idx < 0 {
			t.Fatalf("unsupported capability without a reason: %q", line)
		}
		columns = append(columns, idx)
	}
	if len(columns) < 2 {
		t.Fatalf("expected several unsupported capabilities, got %d", len(columns))
	}
	for _, c := range columns[1:] {
		if c != columns[0] {
			t.Errorf("reason column %d != %d; reasons are not aligned", c, columns[0])
		}
	}
}

// A supported capability prints no reason; an unsupported one always does.
func TestRenderHealth_MarksAndReasons(t *testing.T) {
	h := zotero.Health{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true, LocalAPIEnabled: true}
	lines := capabilityLines(t, renderToString(h))

	for _, line := range lines {
		switch {
		case strings.Contains(line, "✓"):
			if fields := strings.Fields(line); len(fields) != 2 {
				t.Errorf("supported capability carries extra text: %q", line)
			}
		case strings.Contains(line, "✗"):
			if len(strings.Fields(line)) < 3 {
				t.Errorf("unsupported capability lacks a reason: %q", line)
			}
		default:
			t.Errorf("capability line has no mark: %q", line)
		}
	}
}

// The endpoint kind belongs in the header: it is the thing v0.5 will vary.
func TestRenderHealth_NamesTheEndpoint(t *testing.T) {
	out := renderToString(zotero.Health{Endpoint: zotero.LocalProfile("http://localhost:23119")})
	if !strings.Contains(out, "local endpoint at http://localhost:23119") {
		t.Errorf("header does not name the endpoint:\n%s", out)
	}
}

// The actionable fix must survive the capability section.
func TestRenderHealth_KeepsActionableGuidance(t *testing.T) {
	down := renderToString(zotero.Health{Endpoint: zotero.LocalProfile("http://x")})
	if !strings.Contains(down, "Start the Zotero 7+ desktop app") {
		t.Errorf("missing start guidance:\n%s", down)
	}

	off := renderToString(zotero.Health{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true})
	if !strings.Contains(off, "Settings → Advanced → General") {
		t.Errorf("missing enable guidance:\n%s", off)
	}

	ready := renderToString(zotero.Health{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true, LocalAPIEnabled: true})
	if !strings.Contains(ready, "Ready.") {
		t.Errorf("missing ready line:\n%s", ready)
	}
}

func renderLibrariesToString(libs []zotero.LibraryFiles) string {
	var buf bytes.Buffer
	renderLibraries(&buf, libs)
	return buf.String()
}

// A library that accepts files gets a bare ✓; one that does not gets a ✗ and an
// explanation, so a silent no-attachments library is no longer a mystery.
func TestRenderLibraries_MarksAndReason(t *testing.T) {
	out := renderLibrariesToString([]zotero.LibraryFiles{
		{ID: 1, Name: "My Library", FilesEditable: true},
		{ID: 6, Name: "Biological Reactor", FilesEditable: false},
	})
	if !strings.Contains(out, "My Library") || !strings.Contains(out, "files ✓") {
		t.Errorf("missing the files-editable library:\n%s", out)
	}
	if !strings.Contains(out, "Biological Reactor") || !strings.Contains(out, "files ✗") {
		t.Errorf("missing the files-not-editable library:\n%s", out)
	}
	if !strings.Contains(out, "attachments not accepted") {
		t.Errorf("a ✗ library must explain itself:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

// The Libraries section must come before the closing summary, so "Ready." reads
// as genuinely last (the report otherwise looks like it ended, then resumed).
func TestRenderHealth_LibrariesBeforeSummary(t *testing.T) {
	var buf bytes.Buffer
	h := zotero.Health{Endpoint: zotero.LocalProfile("http://x"), ZoteroRunning: true, LocalAPIEnabled: true}
	renderHealth(&buf, h, []zotero.LibraryFiles{{ID: 1, Name: "My Library", FilesEditable: true}})
	out := buf.String()
	libIdx := strings.Index(out, "Libraries:")
	readyIdx := strings.Index(out, "Ready.")
	if libIdx < 0 || readyIdx < 0 {
		t.Fatalf("missing Libraries or Ready:\n%s", out)
	}
	if libIdx > readyIdx {
		t.Errorf("Libraries section must precede the Ready summary:\n%s", out)
	}
}

// Nothing to report must render nothing, so a Web-API or probe-failed run does
// not print an empty section.
func TestRenderLibraries_EmptyIsSilent(t *testing.T) {
	if out := renderLibrariesToString(nil); out != "" {
		t.Errorf("expected no output for no libraries, got %q", out)
	}
}

// The web endpoint's guidance points at the API key, not the desktop app.
func TestRenderHealth_WebGuidance(t *testing.T) {
	rejected := renderToString(zotero.Health{Endpoint: zotero.WebProfile("https://api.zotero.org"), Reachable: true})
	if !strings.Contains(rejected, "API key rejected") || !strings.Contains(rejected, "ZOTGO_API_KEY") {
		t.Errorf("missing rejected-key guidance:\n%s", rejected)
	}

	unreachable := renderToString(zotero.Health{Endpoint: zotero.WebProfile("https://api.zotero.org")})
	if !strings.Contains(unreachable, "unreachable") {
		t.Errorf("missing unreachable guidance:\n%s", unreachable)
	}
}

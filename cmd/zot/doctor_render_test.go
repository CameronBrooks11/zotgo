package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func renderToString(h zotero.Health) string {
	var buf bytes.Buffer
	renderHealth(&buf, h)
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

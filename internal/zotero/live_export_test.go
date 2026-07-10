//go:build live

// Live export tests check the one thing the httptest fakes cannot: that the
// per-format merge strategies match how Zotero's translators actually behave at
// page boundaries. The fakes encode our reading of each format; only a real
// Zotero can falsify it.
//
// Every test scopes its export to a handful of item keys (`itemKey=`) so that
// `limit=1` forces genuine page boundaries without paginating the whole library.
// An earlier version forced pagination across all top-level items and skipped
// whenever the library was large — which is to say, always.
//
//	go test -tags live ./internal/zotero -run TestLiveExport -v
package zotero

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"testing"
)

// sampleKeys returns the keys of n top-level items, or skips when the library
// holds too few to exercise the merge.
func sampleKeys(t *testing.T, c *Client, n int) []string {
	t.Helper()
	items, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{Top: true, Limit: n})
	if err != nil {
		t.Fatalf("sampling items: %v", err)
	}
	if len(items) < n {
		t.Skipf("need %d top-level items to force pagination, have %d", n, len(items))
	}
	keys := make([]string, 0, n)
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	return keys
}

// paged scopes an export to keys, one item per page, so it spans len(keys) pages.
func paged(keys []string) ItemsOptions {
	return ItemsOptions{Top: true, ItemKeys: keys, Limit: 1}
}

// singlePage scopes an export to keys that all fit in one page.
func singlePage(keys []string) ItemsOptions {
	return ItemsOptions{Top: true, ItemKeys: keys, Limit: 100}
}

// countLinesWithPrefix counts records by their opening line, so a record lost at
// a page seam shows up as a short count.
func countLinesWithPrefix(doc []byte, prefix string) int {
	n := 0
	for _, line := range bytes.Split(doc, []byte("\n")) {
		if bytes.HasPrefix(line, []byte(prefix)) {
			n++
		}
	}
	return n
}

// Record formats concatenate. Every page's records must survive the merge.
func TestLiveExportRecordFormatsConcatenate(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 3)

	for _, tc := range []struct{ format, prefix string }{
		{"bibtex", "@"},
		{"biblatex", "@"},
		{"ris", "TY  - "},
	} {
		t.Run(tc.format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), paged(keys), tc.format)
			if err != nil {
				t.Fatalf("Export(%s): %v", tc.format, err)
			}
			if got := countLinesWithPrefix(out, tc.prefix); got != len(keys) {
				t.Errorf("%s: %d records across %d pages, want %d\n%s",
					tc.format, got, len(keys), len(keys), truncateBytes(out, 600))
			}
		})
	}
}

// csljson pages are arrays; merged they must form one array of every record.
func TestLiveExportCSLJSONMergesToOneArray(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 3)

	out, err := c.Export(context.Background(), UserLibrary(), paged(keys), "csljson")
	if err != nil {
		t.Fatalf("Export(csljson): %v", err)
	}
	var merged []map[string]any
	if err := json.Unmarshal(out, &merged); err != nil {
		t.Fatalf("merged csljson is not one array: %v\n%s", err, truncateBytes(out, 400))
	}
	if len(merged) != len(keys) {
		t.Errorf("merged %d records, want %d", len(merged), len(keys))
	}
}

// The claim under test: Zotero's native CSV repeats its header on every page,
// and mergeCSV drops all but the first. If Zotero does NOT repeat it, this test
// fails with a row count one short per page — which is the bug we want to hear
// about.
func TestLiveExportNativeCSVHasExactlyOneHeader(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 3)

	out, err := c.Export(context.Background(), UserLibrary(), paged(keys), "csv")
	if err != nil {
		t.Fatalf("Export(csv): %v", err)
	}
	records, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("merged csv does not parse: %v\n%s", err, truncateBytes(out, 400))
	}
	if len(records) != len(keys)+1 {
		t.Fatalf("got %d csv records, want %d rows + 1 header", len(records), len(keys))
	}

	header := records[0]
	for i, row := range records[1:] {
		if len(row) > 1 && len(header) > 1 && row[0] == header[0] && row[1] == header[1] {
			t.Fatalf("row %d is a repeated header: %v", i, row)
		}
	}
}

// XML formats wrap each page in one root element, so a multi-page export must be
// refused rather than emitted with several roots.
func TestLiveExportXMLRefusesMultiplePages(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 2)

	for _, format := range []string{"mods", "tei", "rdf_zotero", "rdf_dc", "rdf_bibliontology"} {
		t.Run(format, func(t *testing.T) {
			_, err := c.Export(context.Background(), UserLibrary(), paged(keys), format)
			if !errors.Is(err, ErrUnmergeableExport) {
				t.Fatalf("err = %v, want ErrUnmergeableExport", err)
			}
		})
	}
}

// One page of XML must pass through as a well-formed document with a single root.
// This is the other half of the refusal above: the reason two pages cannot merge
// is that each page is already a complete document.
func TestLiveExportXMLSinglePageIsWellFormed(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 3)

	for _, format := range []string{"mods", "tei", "rdf_zotero", "rdf_dc", "rdf_bibliontology"} {
		t.Run(format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), singlePage(keys), format)
			if err != nil {
				t.Fatalf("Export(%s): %v", format, err)
			}
			if roots := countXMLRoots(t, out); roots != 1 {
				t.Fatalf("%s: document has %d root elements, want 1\n%s",
					format, roots, truncateBytes(out, 400))
			}
		})
	}
}

// countXMLRoots parses the document and counts top-level elements, which is what
// makes a concatenation of pages invalid.
func countXMLRoots(t *testing.T, doc []byte) int {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(doc))
	depth, roots := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return roots
		}
		if err != nil {
			t.Fatalf("xml is not well-formed: %v", err)
		}
		switch tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

// Every advertised format must actually be accepted by this Zotero, or the
// registry is lying about what it supports. Scoped to one page so the XML
// formats stay within their single-page ceiling.
func TestLiveExportEveryAdvertisedFormatIsAccepted(t *testing.T) {
	c := liveClient(t)
	keys := sampleKeys(t, c, 3)

	for _, format := range ExportFormats() {
		t.Run(format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), singlePage(keys), format)
			if err != nil {
				t.Fatalf("Zotero rejected advertised format %q: %v", format, err)
			}
			if len(bytes.TrimSpace(out)) == 0 {
				t.Errorf("format %q produced empty output", format)
			}
		})
	}
}

func truncateBytes(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

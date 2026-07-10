//go:build live

// Live export tests check the one thing the httptest fakes cannot: that the
// per-format merge strategies match how Zotero's translators actually behave at
// page boundaries. The fakes encode our reading of each format; only a real
// Zotero can falsify it.
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

// maxPagedItems bounds the multi-page tests. They force pagination with limit=1,
// which costs one HTTP request per item, so a large library is skipped rather
// than hammered.
const maxPagedItems = 30

// topItemCount reports how many top-level items the user library holds.
func topItemCount(t *testing.T, c *Client) int {
	t.Helper()
	_, page, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{Top: true, Limit: 1})
	if err != nil {
		t.Fatalf("counting items: %v", err)
	}
	return page.TotalResults
}

// pagedOpts forces one item per page, so every export crosses page boundaries.
func pagedOpts() ItemsOptions { return ItemsOptions{Top: true, Limit: 1} }

func requirePaged(t *testing.T, c *Client) int {
	t.Helper()
	n := topItemCount(t, c)
	switch {
	case n < 2:
		t.Skipf("need at least 2 top-level items to force pagination, have %d", n)
	case n > maxPagedItems:
		t.Skipf("library has %d top-level items; limit=1 would issue %d requests", n, n)
	}
	return n
}

// Record formats concatenate. Every page's records must survive.
func TestLiveExportRecordFormatsConcatenate(t *testing.T) {
	c := liveClient(t)
	n := requirePaged(t, c)

	for _, tc := range []struct{ format, marker string }{
		{"bibtex", "@"},
		{"biblatex", "@"},
		{"ris", "TY  - "},
	} {
		t.Run(tc.format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), pagedOpts(), tc.format)
			if err != nil {
				t.Fatalf("Export(%s): %v", tc.format, err)
			}
			if got := bytes.Count(out, []byte(tc.marker)); got < n {
				t.Errorf("%s: found %d records across %d pages, want >= %d\n%s",
					tc.format, got, n, n, truncateBytes(out, 400))
			}
		})
	}
}

// csljson pages are arrays; merged they must form one array of every record.
func TestLiveExportCSLJSONMergesToOneArray(t *testing.T) {
	c := liveClient(t)
	n := requirePaged(t, c)

	out, err := c.Export(context.Background(), UserLibrary(), pagedOpts(), "csljson")
	if err != nil {
		t.Fatalf("Export(csljson): %v", err)
	}
	var merged []map[string]any
	if err := json.Unmarshal(out, &merged); err != nil {
		t.Fatalf("merged csljson is not one array: %v\n%s", err, truncateBytes(out, 400))
	}
	if len(merged) != n {
		t.Errorf("merged %d records, want %d", len(merged), n)
	}
}

// The claim under test: Zotero's native CSV repeats its header on every page,
// and mergeCSV drops all but the first. If Zotero does NOT repeat it, this test
// fails with a row count one short per page — which is the bug we want to hear
// about.
func TestLiveExportNativeCSVHasExactlyOneHeader(t *testing.T) {
	c := liveClient(t)
	n := requirePaged(t, c)

	out, err := c.Export(context.Background(), UserLibrary(), pagedOpts(), "csv")
	if err != nil {
		t.Fatalf("Export(csv): %v", err)
	}
	records, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("merged csv does not parse: %v\n%s", err, truncateBytes(out, 400))
	}
	if len(records) != n+1 {
		t.Fatalf("got %d csv records, want %d rows + 1 header", len(records), n)
	}

	header := records[0]
	for i, row := range records[1:] {
		if len(row) > 0 && len(header) > 0 && row[0] == header[0] && row[1] == header[1] {
			t.Fatalf("row %d is a repeated header: %v", i, row)
		}
	}
	t.Logf("native csv header: %v", header)
}

// XML formats wrap each page in one root element, so a multi-page export must be
// refused rather than emitted with several roots.
func TestLiveExportXMLRefusesMultiplePages(t *testing.T) {
	c := liveClient(t)
	requirePaged(t, c)

	for _, format := range []string{"mods", "tei", "rdf_zotero"} {
		t.Run(format, func(t *testing.T) {
			_, err := c.Export(context.Background(), UserLibrary(), pagedOpts(), format)
			if !errors.Is(err, ErrUnmergeableExport) {
				t.Fatalf("err = %v, want ErrUnmergeableExport", err)
			}
		})
	}
}

// One page of XML must pass through as a well-formed document with a single root.
func TestLiveExportXMLSinglePageIsWellFormed(t *testing.T) {
	c := liveClient(t)
	n := topItemCount(t, c)
	if n == 0 {
		t.Skip("empty library")
	}
	// 100 is the Local API's page ceiling; beyond it the export spans pages.
	if n > 100 {
		t.Skipf("library has %d top-level items; a single-page XML export is impossible", n)
	}
	opts := ItemsOptions{Top: true, Limit: 100}

	for _, format := range []string{"mods", "tei", "rdf_zotero"} {
		t.Run(format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), opts, format)
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
// registry is lying about what it supports.
//
// Export always walks to the last page, so a limit of 1 would fetch the whole
// library one item at a time and push the XML formats over their single-page
// ceiling. Request the maximum page size instead, and skip a library too large
// to fit in one page.
func TestLiveExportEveryAdvertisedFormatIsAccepted(t *testing.T) {
	c := liveClient(t)
	n := topItemCount(t, c)
	if n == 0 {
		t.Skip("empty library")
	}
	if n > 100 {
		t.Skipf("library has %d top-level items; XML formats cannot fit one page", n)
	}
	opts := ItemsOptions{Top: true, Limit: 100}

	for _, format := range ExportFormats() {
		t.Run(format, func(t *testing.T) {
			out, err := c.Export(context.Background(), UserLibrary(), opts, format)
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

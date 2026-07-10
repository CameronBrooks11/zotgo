package zotero

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pagedFormatServer serves len(pages) pages for an items/top query, chaining
// them with Link rel="next" cursors that actually advance.
func pagedFormatServer(t *testing.T, pages []string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items/top", func(w http.ResponseWriter, r *http.Request) {
		idx := 0
		if s := r.URL.Query().Get("start"); s != "" {
			_, _ = fmt.Sscanf(s, "%d", &idx)
		}
		if idx < len(pages)-1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items/top?start=%d>; rel="next"`, srv.URL, idx+1))
		}
		_, _ = w.Write([]byte(pages[idx]))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func exportAll(t *testing.T, srv *httptest.Server, format string) ([]byte, error) {
	t.Helper()
	return New(srv.URL).Export(context.Background(), UserLibrary(), ItemsOptions{Top: true}, format)
}

// csvPage renders a page the way Zotero's CSV translator really does: a UTF-8
// BOM, then quoted fields. The BOM is the part that matters — without it these
// tests pass against a document Zotero never sends.
func csvPage(rows ...string) string {
	return "\ufeff" + strings.Join(rows, "\n") + "\n"
}

// parseMergedCSV asserts the merged document carries exactly one leading BOM,
// then parses what follows.
func parseMergedCSV(t *testing.T, got []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(got, utf8BOM) {
		t.Fatalf("merged csv lost Zotero's UTF-8 BOM: %q", truncate(got))
	}
	rest := bytes.TrimPrefix(got, utf8BOM)
	if bytes.Contains(rest, utf8BOM) {
		t.Fatalf("merged csv carries a BOM from a later page: %q", truncate(got))
	}
	records, err := csv.NewReader(bytes.NewReader(rest)).ReadAll()
	if err != nil {
		t.Fatalf("merged csv does not parse: %v\n%s", err, got)
	}
	return records
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

// Zotero's native CSV repeats its header on every page. The merged document
// must carry exactly one.
func TestExport_NativeCSVKeepsOneHeader(t *testing.T) {
	srv := pagedFormatServer(t, []string{
		csvPage(`"Key","Title"`, `"AAAA","Alpha"`),
		csvPage(`"Key","Title"`, `"BBBB","Beta"`),
	})

	records := parseMergedCSV(t, mustExport(t, srv, "csv"))
	if len(records) != 3 {
		t.Fatalf("got %d records, want header + 2 rows: %v", len(records), records)
	}
	if records[0][0] != "Key" {
		t.Fatalf("first record is not the header: %v", records[0])
	}
	for _, r := range records[1:] {
		if r[0] == "Key" {
			t.Fatalf("duplicate header survived: %v", records)
		}
	}
}

// A CSV field containing an embedded newline must not confuse header stripping.
func TestExport_NativeCSVHandlesQuotedNewlines(t *testing.T) {
	srv := pagedFormatServer(t, []string{
		csvPage(`"Key","Title"`, "\"AAAA\",\"Alpha\nwrapped\""),
		csvPage(`"Key","Title"`, `"BBBB","Beta"`),
	})

	records := parseMergedCSV(t, mustExport(t, srv, "csv"))
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3: %v", len(records), records)
	}
	if records[1][1] != "Alpha\nwrapped" {
		t.Fatalf("embedded newline mangled: %q", records[1][1])
	}
}

// If a later page does not repeat the header, its first row is a real item.
// Dropping it unconditionally would silently lose data.
func TestExport_NativeCSVKeepsFirstRowWhenHeaderNotRepeated(t *testing.T) {
	srv := pagedFormatServer(t, []string{
		csvPage(`"Key","Title"`, `"AAAA","Alpha"`),
		csvPage(`"BBBB","Beta"`), // no header on this page
	})

	records := parseMergedCSV(t, mustExport(t, srv, "csv"))
	if len(records) != 3 {
		t.Fatalf("got %d records, want header + 2 rows (an item was dropped): %v", len(records), records)
	}
	if records[2][0] != "BBBB" {
		t.Fatalf("second page's item was dropped as a header: %v", records)
	}
}

func mustExport(t *testing.T, srv *httptest.Server, format string) []byte {
	t.Helper()
	got, err := exportAll(t, srv, format)
	if err != nil {
		t.Fatalf("Export(%s): %v", format, err)
	}
	return got
}

// One page of XML is fine: pass it straight through.
func TestExport_SinglePageXMLPassesThrough(t *testing.T) {
	doc := `<modsCollection><mods><titleInfo>A</titleInfo></mods></modsCollection>`
	srv := pagedFormatServer(t, []string{doc})

	got, err := exportAll(t, srv, "mods")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if string(got) != doc {
		t.Fatalf("payload altered:\n%s", got)
	}
}

// Concatenating XML pages would produce two root elements. Refuse instead.
func TestExport_MultiPageXMLIsRefused(t *testing.T) {
	for _, format := range []string{"mods", "tei", "rdf_zotero"} {
		t.Run(format, func(t *testing.T) {
			srv := pagedFormatServer(t, []string{"<root><a/></root>", "<root><b/></root>"})

			_, err := exportAll(t, srv, format)
			if !errors.Is(err, ErrUnmergeableExport) {
				t.Fatalf("err = %v, want ErrUnmergeableExport", err)
			}
			if !strings.Contains(err.Error(), format) {
				t.Fatalf("error does not name the format: %v", err)
			}
		})
	}
}

func TestExport_UnknownFormatIsRejected(t *testing.T) {
	srv := pagedFormatServer(t, []string{"[]"})

	_, err := exportAll(t, srv, "nonsense")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
	// The message should tell the user what they can pick instead.
	if !strings.Contains(err.Error(), "bibtex") {
		t.Fatalf("error does not list supported formats: %v", err)
	}
}

func TestExportFormats_AreSortedAndNonEmpty(t *testing.T) {
	got := ExportFormats()
	if len(got) == 0 {
		t.Fatal("ExportFormats() is empty")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("ExportFormats() not sorted: %v", got)
		}
	}
}

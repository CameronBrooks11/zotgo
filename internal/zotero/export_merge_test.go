package zotero

import (
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

// Zotero's native CSV repeats its header on every page. The merged document
// must carry exactly one.
func TestExport_NativeCSVKeepsOneHeader(t *testing.T) {
	page := "Key,Title\n%s,%s\n"
	srv := pagedFormatServer(t, []string{
		fmt.Sprintf(page, "AAAA", "Alpha"),
		fmt.Sprintf(page, "BBBB", "Beta"),
	})

	got, err := exportAll(t, srv, "csv")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(string(got))).ReadAll()
	if err != nil {
		t.Fatalf("merged csv does not parse: %v\n%s", err, got)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want header + 2 rows:\n%s", len(records), got)
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
		"Key,Title\nAAAA,\"Alpha\nwrapped\"\n",
		"Key,Title\nBBBB,Beta\n",
	})

	got, err := exportAll(t, srv, "csv")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(got))).ReadAll()
	if err != nil {
		t.Fatalf("merged csv does not parse: %v\n%s", err, got)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3:\n%s", len(records), got)
	}
	if records[1][1] != "Alpha\nwrapped" {
		t.Fatalf("embedded newline mangled: %q", records[1][1])
	}
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

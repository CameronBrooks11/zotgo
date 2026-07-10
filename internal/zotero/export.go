package zotero

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// mergeFunc combines the per-page bodies of a paginated export into one
// document. It receives at least one page.
type mergeFunc func(format string, pages [][]byte) ([]byte, error)

// exportFormats maps a Zotero translator name to the way its pages combine.
//
// Zotero paginates every item query, including exported ones, so a format is
// only usable here if its pages can be reassembled into one valid document.
// That is a property of the format, not of the transport, so it is recorded
// per format rather than guessed at call time.
var exportFormats = map[string]mergeFunc{
	// Record-per-entry text: pages append.
	"bibtex":   mergeConcat,
	"biblatex": mergeConcat,
	"ris":      mergeConcat,

	// A JSON array per page: splice into one array.
	"csljson": mergeJSONArray,

	// A header row per page: keep the first, drop the rest.
	"csv": mergeCSV,

	// One XML root element per page. Two roots is not a document, so these
	// merge only when the result fits in a single page.
	"mods":              mergeSinglePage,
	"tei":               mergeSinglePage,
	"rdf_bibliontology": mergeSinglePage,
	"rdf_dc":            mergeSinglePage,
	"rdf_zotero":        mergeSinglePage,
}

// ExportFormats lists the server-side translator names Export accepts, sorted.
func ExportFormats() []string {
	names := make([]string, 0, len(exportFormats))
	for name := range exportFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Export returns the items matching opts rendered by one of Zotero's own
// translators (format=…), reassembled across pages.
//
// Zotero does the formatting; zotgo only rejoins what pagination split. A
// format whose pages cannot be rejoined into a valid document yields
// ErrUnmergeableExport rather than corrupt output.
func (c *Client) Export(ctx context.Context, library LibraryRef, opts ItemsOptions, format string) ([]byte, error) {
	merge, ok := exportFormats[format]
	if !ok {
		return nil, fmt.Errorf("%w: %q (want %s)", ErrUnsupportedFormat, format, strings.Join(ExportFormats(), ", "))
	}
	pages, err := c.exportPages(ctx, library, opts, format)
	if err != nil {
		return nil, err
	}
	return merge(format, pages)
}

// mergeConcat joins record-oriented text pages, separated by a blank line.
func mergeConcat(_ string, pages [][]byte) ([]byte, error) {
	trimmed := make([][]byte, 0, len(pages))
	for _, p := range pages {
		if t := bytes.TrimSpace(p); len(t) > 0 {
			trimmed = append(trimmed, t)
		}
	}
	joined := bytes.Join(trimmed, []byte("\n\n"))
	if len(joined) > 0 {
		joined = append(joined, '\n')
	}
	return joined, nil
}

// mergeJSONArray splices each page's top-level array into one array.
func mergeJSONArray(_ string, pages [][]byte) ([]byte, error) {
	merged := make([]json.RawMessage, 0)
	for _, p := range pages {
		if len(bytes.TrimSpace(p)) == 0 {
			continue
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(p, &arr); err != nil {
			return nil, err
		}
		merged = append(merged, arr...)
	}
	return json.MarshalIndent(merged, "", "  ")
}

// utf8BOM prefixes every page of Zotero's native CSV so spreadsheet software
// reads the file as UTF-8. encoding/csv does not treat it specially: it becomes
// part of the first header field, and the quote that follows then looks like a
// bare quote in an unquoted field. Strip it per page, re-emit it once.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// mergeCSV keeps the first page's header and drops the one repeating at the top
// of every later page. The rows are parsed rather than split on newlines, since
// a quoted field may contain one.
//
// A later page's first row is dropped only when it actually equals the header.
// Zotero repeats it on every page today, but dropping unconditionally would
// silently delete a real item the day it stops.
func mergeCSV(_ string, pages [][]byte) ([]byte, error) {
	var out [][]string
	var header []string
	for i, p := range pages {
		p = bytes.TrimPrefix(p, utf8BOM)
		if len(bytes.TrimSpace(p)) == 0 {
			continue
		}
		records, err := csv.NewReader(bytes.NewReader(p)).ReadAll()
		if err != nil {
			return nil, fmt.Errorf("parsing csv page %d: %w", i+1, err)
		}
		if len(records) == 0 {
			continue
		}
		if header == nil {
			header = records[0]
		} else if slices.Equal(records[0], header) {
			records = records[1:]
		}
		out = append(out, records...)
	}

	if len(out) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	buf.Write(utf8BOM)
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), w.Error()
}

// mergeSinglePage passes one page through untouched and refuses more, because
// concatenating documents that each carry a root element yields no document.
func mergeSinglePage(format string, pages [][]byte) ([]byte, error) {
	if len(pages) > 1 {
		return nil, fmt.Errorf(
			"%w: %s wraps each page in its own root element, and this query returned %d pages; narrow it with --collection or --tag",
			ErrUnmergeableExport, format, len(pages))
	}
	return pages[0], nil
}

// exportPages fetches every page of an item query with a server-side format,
// returning the raw page bodies in order.
func (c *Client) exportPages(ctx context.Context, library LibraryRef, opts ItemsOptions, format string) ([][]byte, error) {
	path := itemsPath(library, opts)
	var pages [][]byte
	for {
		values := itemValues(opts)
		if values == nil {
			values = url.Values{}
		}
		values.Set("format", format)

		body, page, err := c.do(ctx, path, values)
		if err != nil {
			return nil, err
		}
		pages = append(pages, body)

		start, more, err := nextStart(page.NextURL, opts.Start)
		if err != nil {
			return nil, err
		}
		if !more {
			return pages, nil
		}
		opts.Start = start
	}
}

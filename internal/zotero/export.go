package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
)

// ExportBibtex returns the items matching opts as BibTeX, produced server-side
// by Zotero (format=bibtex) and concatenated across pages.
func (c *Client) ExportBibtex(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]byte, error) {
	pages, err := c.exportPages(ctx, library, opts, "bibtex")
	if err != nil {
		return nil, err
	}
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

// ExportCSLJSON returns the items matching opts as CSL-JSON, produced
// server-side (format=csljson) and merged into one array across pages.
func (c *Client) ExportCSLJSON(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]byte, error) {
	pages, err := c.exportPages(ctx, library, opts, "csljson")
	if err != nil {
		return nil, err
	}
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

		start, more := nextStart(page.NextURL)
		if !more {
			return pages, nil
		}
		opts.Start = start
	}
}

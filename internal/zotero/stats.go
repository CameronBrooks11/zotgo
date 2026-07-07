package zotero

import (
	"context"
	"net/url"
)

// Stats holds library-wide counts derived from Total-Results headers.
type Stats struct {
	Items       int
	TopItems    int
	Collections int
	Tags        int
}

// Stats returns library-wide counts. It issues one cheap limit=1 request per
// count and reads the Total-Results header, never paging the actual rows.
func (c *Client) Stats(ctx context.Context, library LibraryRef) (Stats, error) {
	prefix := library.Prefix()
	counts := []struct {
		path string
		dst  *int
	}{
		{prefix + "/items", nil},
		{prefix + "/items/top", nil},
		{prefix + "/collections", nil},
		{prefix + "/tags", nil},
	}
	var s Stats
	counts[0].dst = &s.Items
	counts[1].dst = &s.TopItems
	counts[2].dst = &s.Collections
	counts[3].dst = &s.Tags

	for _, cnt := range counts {
		n, err := c.count(ctx, cnt.path)
		if err != nil {
			return s, err
		}
		*cnt.dst = n
	}
	return s, nil
}

// count returns the Total-Results header for a limit=1 request against path.
func (c *Client) count(ctx context.Context, path string) (int, error) {
	page, err := c.getJSON(ctx, path, url.Values{"limit": {"1"}}, nil)
	if err != nil {
		return 0, err
	}
	return page.TotalResults, nil
}

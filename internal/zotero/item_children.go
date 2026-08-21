package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// ChildItemsOptions controls requests for the children of an item.
type ChildItemsOptions struct {
	ItemType string
	Limit    int
	Start    int
}

// RawChildItems reads one page of complete child item envelopes. It validates
// only each child's identity, leaving all other Zotero-owned fields untouched.
func (c *Client) RawChildItems(ctx context.Context, library LibraryRef, parentKey string, opts ChildItemsOptions) ([]json.RawMessage, Page, error) {
	if parentKey == "" {
		return nil, Page{}, errors.New("parent item key is empty")
	}

	path := c.profile.LibraryPrefix(library) + "/items/" + url.PathEscape(parentKey) + "/children"
	body, page, err := c.do(ctx, path, childItemsValues(opts))
	if err != nil {
		return nil, page, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, page, errors.New("decode child items: response body is empty")
	}
	if trimmed[0] != '[' {
		return nil, page, errors.New("decode child items: response is not a JSON array")
	}

	children := make([]json.RawMessage, 0)
	if err := json.Unmarshal(trimmed, &children); err != nil {
		return nil, page, fmt.Errorf("decode child items: %w", err)
	}
	for i, child := range children {
		identity, err := DecodeItemIdentity(child)
		if err != nil {
			return nil, page, fmt.Errorf("decode child item %d: %w", i, err)
		}
		if identity.Key == parentKey {
			return nil, page, fmt.Errorf("decode child item %d: child repeats parent key %q", i, parentKey)
		}
	}
	return children, page, nil
}

// AllRawChildItems follows Link rel="next" and returns every complete child
// item envelope in server order. It returns no partial result if any page fails.
func (c *Client) AllRawChildItems(ctx context.Context, library LibraryRef, parentKey string, opts ChildItemsOptions) ([]json.RawMessage, error) {
	all := make([]json.RawMessage, 0)
	for {
		children, page, err := c.RawChildItems(ctx, library, parentKey, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, children...)

		start, more, err := nextStart(page.NextURL, opts.Start)
		if err != nil {
			return nil, err
		}
		if !more {
			return all, nil
		}
		if err := c.honorBackoff(ctx, page); err != nil {
			return nil, err
		}
		opts.Start = start
	}
}

func childItemsValues(opts ChildItemsOptions) url.Values {
	values := url.Values{}
	if opts.ItemType != "" {
		values.Set("itemType", opts.ItemType)
	}
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Start > 0 {
		values.Set("start", strconv.Itoa(opts.Start))
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Page carries pagination and version headers for a Local API response.
type Page struct {
	TotalResults        int
	NextURL             string
	LastModifiedVersion string
	SchemaVersion       string
	APIVersion          string
}

// ItemsOptions controls item-list and search requests.
type ItemsOptions struct {
	Top        bool
	Collection string
	Tags       []string
	Limit      int
	Start      int
	Query      string
	Everything bool
	ItemType   string
}

// CollectionsOptions controls collection-list requests.
type CollectionsOptions struct {
	Top   bool
	Start int
}

// Items reads a single page of items.
func (c *Client) Items(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]Envelope, Page, error) {
	path := library.Prefix() + "/items"
	if opts.Collection != "" {
		path = library.Prefix() + "/collections/" + url.PathEscape(opts.Collection) + "/items"
	}
	if opts.Top {
		path += "/top"
	}
	var items []Envelope
	page, err := c.getJSON(ctx, path, itemValues(opts), &items)
	return items, page, err
}

// AllItems follows Link rel="next" and returns the full result set.
func (c *Client) AllItems(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]Envelope, error) {
	var all []Envelope
	for {
		items, page, err := c.Items(ctx, library, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		start, more := nextStart(page.NextURL)
		if !more {
			return all, nil
		}
		opts.Start = start
	}
}

// Item reads one item by key.
func (c *Client) Item(ctx context.Context, library LibraryRef, key string) (Envelope, error) {
	var item Envelope
	_, err := c.getJSON(ctx, library.Prefix()+"/items/"+url.PathEscape(key), nil, &item)
	return item, err
}

// ItemChildren reads attachments and notes under a parent item.
func (c *Client) ItemChildren(ctx context.Context, library LibraryRef, key string) ([]Envelope, Page, error) {
	var children []Envelope
	page, err := c.getJSON(ctx, library.Prefix()+"/items/"+url.PathEscape(key)+"/children", nil, &children)
	return children, page, err
}

// Collections reads a single page of collections.
func (c *Client) Collections(ctx context.Context, library LibraryRef, opts CollectionsOptions) ([]Envelope, Page, error) {
	path := library.Prefix() + "/collections"
	if opts.Top {
		path += "/top"
	}
	var values url.Values
	if opts.Start > 0 {
		values = url.Values{"start": {strconv.Itoa(opts.Start)}}
	}
	var collections []Envelope
	page, err := c.getJSON(ctx, path, values, &collections)
	return collections, page, err
}

// AllCollections follows Link rel="next" and returns every collection.
func (c *Client) AllCollections(ctx context.Context, library LibraryRef, opts CollectionsOptions) ([]Envelope, error) {
	var all []Envelope
	for {
		cols, page, err := c.Collections(ctx, library, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, cols...)
		start, more := nextStart(page.NextURL)
		if !more {
			return all, nil
		}
		opts.Start = start
	}
}

func itemValues(opts ItemsOptions) url.Values {
	v := url.Values{}
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Start > 0 {
		v.Set("start", strconv.Itoa(opts.Start))
	}
	if opts.Query != "" {
		v.Set("q", opts.Query)
		if opts.Everything {
			v.Set("qmode", "everything")
		}
	}
	if opts.ItemType != "" {
		v.Set("itemType", opts.ItemType)
	}
	for _, tag := range opts.Tags {
		if tag != "" {
			v.Add("tag", tag)
		}
	}
	if len(v) == 0 {
		return nil
	}
	return v
}

func (c *Client) getJSON(ctx context.Context, path string, values url.Values, out any) (Page, error) {
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	resp, err := c.get(ctx, path)
	if err != nil {
		return Page{}, errors.Join(ErrZoteroDown, err)
	}
	defer resp.Body.Close()

	page := pageFromHeader(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		switch resp.StatusCode {
		case http.StatusForbidden:
			if strings.Contains(string(body), "Local API is not enabled") || len(body) == 0 {
				return page, ErrLocalAPIDisabled
			}
		case http.StatusNotFound:
			return page, ErrNotFound
		}
		return page, StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if out == nil {
		return page, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return page, err
	}
	return page, nil
}

func pageFromHeader(h http.Header) Page {
	total, _ := strconv.Atoi(h.Get("Total-Results"))
	return Page{
		TotalResults:        total,
		NextURL:             linkRel(h.Get("Link"), "next"),
		LastModifiedVersion: h.Get("Last-Modified-Version"),
		SchemaVersion:       h.Get("Zotero-Schema-Version"),
		APIVersion:          h.Get("Zotero-API-Version"),
	}
}

// nextStart extracts the start offset from a Link rel="next" URL. The second
// return is false when there is no further page.
func nextStart(nextURL string) (int, bool) {
	if nextURL == "" {
		return 0, false
	}
	u, err := url.Parse(nextURL)
	if err != nil {
		return 0, false
	}
	start, _ := strconv.Atoi(u.Query().Get("start"))
	return start, true
}

func linkRel(header, rel string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, `rel="`+rel+`"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}

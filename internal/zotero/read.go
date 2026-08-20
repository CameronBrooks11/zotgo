package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Page carries pagination and version headers for a Local API response.
type Page struct {
	TotalResults        int
	NextURL             string
	LastModifiedVersion string
	SchemaVersion       string
	APIVersion          string
	// Backoff is the Web API's request to pause before further requests when its
	// servers are loaded (the Backoff header). Zero when absent. Pagination loops
	// honor it between pages.
	Backoff time.Duration
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
	// ItemKeys restricts the result to these item keys (`itemKey=`). Combined
	// with Top it selects exactly those items; on the unfiltered /items route
	// Zotero also returns their children.
	ItemKeys []string
}

// CollectionsOptions controls collection-list requests.
type CollectionsOptions struct {
	Top   bool
	Start int
}

// Items reads a single page of items.
func (c *Client) Items(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]Envelope, Page, error) {
	var items []Envelope
	page, err := c.getJSON(ctx, itemsPath(c.profile, library, opts), itemValues(opts), &items)
	return items, page, err
}

// itemsPath is the API path for an item query, honoring the collection scope and
// the top-level filter. The endpoint's Profile fixes the library prefix.
func itemsPath(profile Profile, library LibraryRef, opts ItemsOptions) string {
	prefix := profile.LibraryPrefix(library)
	if opts.Collection != "" {
		path := prefix + "/collections/" + url.PathEscape(opts.Collection) + "/items"
		if opts.Top {
			path += "/top"
		}
		return path
	}
	path := prefix + "/items"
	if opts.Top {
		path += "/top"
	}
	return path
}

// AllItems follows Link rel="next" and returns the full result set.
func (c *Client) AllItems(ctx context.Context, library LibraryRef, opts ItemsOptions) ([]Envelope, error) {
	var all []Envelope
	err := c.IterItems(ctx, library, opts, func(items []Envelope) error {
		all = append(all, items...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// IterItems fetches items one page at a time and passes each page to yield in
// order, so a caller can process a very large library without ever holding all
// of it in memory. Pagination, the start cursor, and Web API backoff are handled
// exactly as AllItems. If yield returns an error, iteration stops and that error
// is returned unchanged; a nil error runs to the last page.
func (c *Client) IterItems(ctx context.Context, library LibraryRef, opts ItemsOptions, yield func([]Envelope) error) error {
	for {
		items, page, err := c.Items(ctx, library, opts)
		if err != nil {
			return err
		}
		if err := yield(items); err != nil {
			return err
		}
		start, more, err := nextStart(page.NextURL, opts.Start)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		if err := c.honorBackoff(ctx, page); err != nil {
			return err
		}
		opts.Start = start
	}
}

// honorBackoff pauses between paginated requests when the Web API asked the
// client to slow down. Capped like a retry so a stray header cannot hang the CLI.
func (c *Client) honorBackoff(ctx context.Context, page Page) error {
	if page.Backoff <= 0 {
		return nil
	}
	wait := page.Backoff
	if wait > maxRetryWait {
		wait = maxRetryWait
	}
	return c.sleep(ctx, wait)
}

// Item reads one item by key.
func (c *Client) Item(ctx context.Context, library LibraryRef, key string) (Envelope, error) {
	var item Envelope
	_, err := c.getJSON(ctx, c.profile.LibraryPrefix(library)+"/items/"+url.PathEscape(key), nil, &item)
	return item, err
}

// RawItem reads one complete item envelope, validating only the response shape
// and requested identity while leaving every other Zotero-owned field untouched.
func (c *Client) RawItem(ctx context.Context, library LibraryRef, key string) (json.RawMessage, error) {
	raw, _, err := c.rawItemPage(ctx, library, key)
	return raw, err
}

// RawItemWithChildren reads one item and every direct child, honoring backoff
// between the parent response and the first child request.
func (c *Client) RawItemWithChildren(ctx context.Context, library LibraryRef, key string) (json.RawMessage, []json.RawMessage, error) {
	item, page, err := c.rawItemPage(ctx, library, key)
	if err != nil {
		return nil, nil, err
	}
	if err := c.honorBackoff(ctx, page); err != nil {
		return nil, nil, err
	}
	children, err := c.allRawItemChildren(ctx, library, key)
	if err != nil {
		return nil, nil, err
	}
	return item, children, nil
}

func (c *Client) rawItemPage(ctx context.Context, library LibraryRef, key string) (json.RawMessage, Page, error) {
	body, page, err := c.do(ctx, c.profile.LibraryPrefix(library)+"/items/"+url.PathEscape(key), nil)
	if err != nil {
		return nil, page, err
	}
	identity, err := DecodeItemIdentity(body)
	if err != nil {
		return nil, page, fmt.Errorf("decode item %q: %w", key, err)
	}
	if identity.Key != key {
		return nil, page, fmt.Errorf("decode item %q: response has key %q", key, identity.Key)
	}
	return json.RawMessage(body), page, nil
}

// Attachment reads and decodes one attachment's bounded metadata.
func (c *Client) Attachment(ctx context.Context, library LibraryRef, key string) (Attachment, error) {
	raw, err := c.RawItem(ctx, library, key)
	if err != nil {
		return Attachment{}, err
	}
	if err := RequireItemType(raw, "attachment"); err != nil {
		return Attachment{}, fmt.Errorf("decode attachment %q: %w", key, err)
	}
	attachment, err := DecodeAttachment(raw)
	if err != nil {
		return Attachment{}, fmt.Errorf("decode attachment %q: %w", key, err)
	}
	return attachment, nil
}

// Collection reads one collection by key.
func (c *Client) Collection(ctx context.Context, library LibraryRef, key string) (Envelope, error) {
	var col Envelope
	_, err := c.getJSON(ctx, c.profile.LibraryPrefix(library)+"/collections/"+url.PathEscape(key), nil, &col)
	return col, err
}

// ItemChildren reads one page of attachments and notes under a parent item.
func (c *Client) ItemChildren(ctx context.Context, library LibraryRef, key string) ([]Envelope, Page, error) {
	var children []Envelope
	page, err := c.getJSON(ctx, c.profile.LibraryPrefix(library)+"/items/"+url.PathEscape(key)+"/children", nil, &children)
	return children, page, err
}

// allRawItemChildren follows pagination and preserves each complete child item
// envelope without decoding Zotero-owned fields.
func (c *Client) allRawItemChildren(ctx context.Context, library LibraryRef, key string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	start := 0
	for {
		children, page, err := c.rawItemChildrenPage(ctx, library, key, start)
		if err != nil {
			return nil, err
		}
		all = append(all, children...)
		next, more, err := nextStart(page.NextURL, start)
		if err != nil {
			return nil, err
		}
		if !more {
			if all == nil {
				all = []json.RawMessage{}
			}
			return all, nil
		}
		if err := c.honorBackoff(ctx, page); err != nil {
			return nil, err
		}
		start = next
	}
}

func (c *Client) rawItemChildrenPage(ctx context.Context, library LibraryRef, key string, start int) ([]json.RawMessage, Page, error) {
	var values url.Values
	if start > 0 {
		values = url.Values{"start": {strconv.Itoa(start)}}
	}
	body, page, err := c.do(ctx, c.profile.LibraryPrefix(library)+"/items/"+url.PathEscape(key)+"/children", values)
	if err != nil {
		return nil, page, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, page, errors.New("decode child items: empty response body")
	}
	if trimmed[0] != '[' {
		return nil, page, errors.New("decode child items: expected a JSON array")
	}
	var children []json.RawMessage
	if err := json.Unmarshal(trimmed, &children); err != nil {
		return nil, page, fmt.Errorf("decode child items: %w", err)
	}
	for i, child := range children {
		identity, err := DecodeItemIdentity(child)
		if err != nil {
			return nil, page, fmt.Errorf("decode child item %d at start %d: %w", i, start, err)
		}
		if identity.Key == key {
			return nil, page, fmt.Errorf("decode child item %d at start %d: response repeats parent key %q", i, start, key)
		}
	}
	if children == nil {
		children = []json.RawMessage{}
	}
	return children, page, nil
}

// Collections reads a single page of collections.
func (c *Client) Collections(ctx context.Context, library LibraryRef, opts CollectionsOptions) ([]Envelope, Page, error) {
	path := c.profile.LibraryPrefix(library) + "/collections"
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
	if len(opts.ItemKeys) > 0 {
		v.Set("itemKey", strings.Join(opts.ItemKeys, ","))
	}
	if len(v) == 0 {
		return nil
	}
	return v
}

// do performs a GET and returns the full response body plus pagination/version
// headers, mapping non-2xx responses to the typed error taxonomy. A 429 or 503
// is retried, honoring Retry-After, up to maxRetries times before it surfaces.
func (c *Client) do(ctx context.Context, path string, values url.Values) ([]byte, Page, error) {
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.get(ctx, path)
		if err != nil {
			return nil, Page{}, classifyTransport(err)
		}
		c.captureServerID(resp.Header)
		page := pageFromHeader(resp.Header)
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			wait := retryAfter(resp.Header)
			if attempt < maxRetries && wait <= maxRetryWait {
				if err := c.sleep(ctx, wait); err != nil {
					return nil, page, err
				}
				continue
			}
			return nil, page, StatusError{StatusCode: resp.StatusCode, Body: snippet(body)}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			switch resp.StatusCode {
			case http.StatusForbidden:
				if len(body) == 0 || strings.Contains(string(body), "Local API is not enabled") {
					return nil, page, ErrLocalAPIDisabled
				}
			case http.StatusNotFound:
				return nil, page, ErrNotFound
			}
			return nil, page, StatusError{StatusCode: resp.StatusCode, Body: snippet(body)}
		}
		if readErr != nil {
			return nil, page, readErr
		}
		return body, page, nil
	}
}

// retryAfter reads a Retry-After header as a whole number of seconds, Zotero's
// form. A missing or unparseable value falls back to defaultBackoff, so a
// throttled request always waits something rather than hammering.
func retryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return defaultBackoff
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultBackoff
}

// classifyTransport sorts a failed round-trip into the error taxonomy.
//
// Cancellation and deadlines are returned unwrapped by any sentinel: they are
// the caller's own doing, and callers must be able to errors.Is them. A refused
// dial is the only signal that authoritatively means Zotero is not listening.
// Anything else broke after a connection existed, so it is a transport fault
// rather than evidence about whether Zotero is running.
func classifyTransport(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return fmt.Errorf("%w: %w", ErrZoteroDown, err)
	}
	return fmt.Errorf("%w: %w", ErrTransport, err)
}

func (c *Client) getJSON(ctx context.Context, path string, values url.Values, out any) (Page, error) {
	body, page, err := c.do(ctx, path, values)
	if err != nil {
		return page, err
	}
	if out == nil || len(body) == 0 {
		return page, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return page, err
	}
	return page, nil
}

// snippet trims a response body for inclusion in an error message.
func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

func pageFromHeader(h http.Header) Page {
	total, _ := strconv.Atoi(h.Get("Total-Results"))
	var backoff time.Duration
	if secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Backoff"))); err == nil && secs > 0 {
		backoff = time.Duration(secs) * time.Second
	}
	return Page{
		TotalResults:        total,
		NextURL:             linkRel(h.Get("Link"), "next"),
		LastModifiedVersion: h.Get("Last-Modified-Version"),
		SchemaVersion:       h.Get("Zotero-Schema-Version"),
		APIVersion:          h.Get("Zotero-API-Version"),
		Backoff:             backoff,
	}
}

// nextStart extracts the start offset from a Link rel="next" URL, given the
// offset of the page that produced it. The second return is false when there is
// no further page.
//
// A cursor that is absent, unparseable, or does not advance past cur yields
// ErrBadPagination: a caller that followed it would request the same page
// forever.
func nextStart(nextURL string, cur int) (int, bool, error) {
	if nextURL == "" {
		return 0, false, nil
	}
	u, err := url.Parse(nextURL)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q: %w", ErrBadPagination, nextURL, err)
	}
	raw := u.Query().Get("start")
	if raw == "" {
		return 0, false, fmt.Errorf("%w: no start offset in %q", ErrBadPagination, nextURL)
	}
	start, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%w: start=%q is not a number", ErrBadPagination, raw)
	}
	if start <= cur {
		return 0, false, fmt.Errorf("%w: start=%d does not advance past %d", ErrBadPagination, start, cur)
	}
	return start, true, nil
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

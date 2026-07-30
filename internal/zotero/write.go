package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AuthorizeTimeout bounds how long Authorize waits for the user to answer
// Zotero's approval modal. It is far longer than a normal request because a
// person has to click Allow or Deny.
const AuthorizeTimeout = 2 * time.Minute

// writeOptions configures a single mutating request.
type writeOptions struct {
	// body is the JSON request body, or nil for a bodyless write (DELETE).
	body []byte
	// ifUnmodifiedSince, when non-nil, sends If-Unmodified-Since-Version — the
	// optimistic-concurrency precondition the local write API requires.
	ifUnmodifiedSince *int
	// keyless omits the local API key. Only /api/local/authorize sets it: that is
	// how the key is obtained, so it cannot require one.
	keyless bool
	// timeout overrides the client's default for this request (0 = default).
	timeout time.Duration
}

// writeRequest performs a mutating request against the local endpoint, applying
// the cached Zotero-Server-ID, the local API key, and any version precondition,
// then maps the write-specific status codes onto the error taxonomy. It returns
// the status and body for the caller to interpret on success (2xx).
func (c *Client) writeRequest(ctx context.Context, method, path string, opts writeOptions) (int, []byte, error) {
	// Every write must echo Zotero-Server-ID (428 without it). Bootstrap it here
	// rather than in the caller, so a write works even when no prior read or
	// Authorize populated the cache — e.g. a fresh process reusing a stored key.
	if err := c.ensureServerID(ctx); err != nil {
		return 0, nil, err
	}

	var body io.Reader
	if opts.body != nil {
		body = bytes.NewReader(opts.body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.profile.BaseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := c.ServerID(); id != "" {
		req.Header.Set("Zotero-Server-ID", id)
	}
	if !opts.keyless {
		c.mu.RLock()
		key := c.localKey
		c.mu.RUnlock()
		if key != "" {
			req.Header.Set("Zotero-API-Key", key)
		}
	}
	if opts.ifUnmodifiedSince != nil {
		req.Header.Set("If-Unmodified-Since-Version", strconv.Itoa(*opts.ifUnmodifiedSince))
	}

	httpClient := c.http
	if opts.timeout > 0 {
		cp := *c.http
		cp.Timeout = opts.timeout
		httpClient = &cp
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, classifyTransport(err)
	}
	defer resp.Body.Close()
	c.captureServerID(resp.Header)
	respBody, readErr := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return resp.StatusCode, respBody, ErrWriteUnauthorized
	case http.StatusPreconditionRequired:
		return resp.StatusCode, respBody, ErrPreconditionRequired
	case http.StatusPreconditionFailed:
		return resp.StatusCode, respBody, ErrPreconditionFailed
	}
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, respBody, nil
}

// ensureServerID makes sure a Zotero-Server-ID has been captured, since writes
// require it. A single GET of the API root is enough — every response on a
// write-capable build carries the header.
func (c *Client) ensureServerID(ctx context.Context) error {
	if c.ServerID() != "" {
		return nil
	}
	_, _, err := c.do(ctx, "/api/", nil)
	return err
}

// authorizeResponse is Zotero's reply to POST /api/local/authorize.
type authorizeResponse struct {
	Key      string `json:"key"`
	Remember bool   `json:"remember"`
	Denied   bool   `json:"denied"`
}

// Authorize obtains a local API key for writes by asking Zotero to prompt the
// user; appName is shown in the modal. On approval it stores the key for
// subsequent writes and reports whether Zotero will remember it (persistent) or
// granted single-use access. Only the local endpoint supports it.
//
// It returns ErrAuthorizeDenied if the user declines. The key is never logged.
func (c *Client) Authorize(ctx context.Context, appName string) (remember bool, err error) {
	if c.profile.Kind != EndpointLocal {
		return false, fmt.Errorf("authorize is only available on the local endpoint")
	}

	reqBody, err := json.Marshal(map[string]string{"appName": appName})
	if err != nil {
		return false, err
	}
	status, respBody, err := c.writeRequest(ctx, http.MethodPost, "/api/local/authorize", writeOptions{
		body:    reqBody,
		keyless: true,
		timeout: AuthorizeTimeout,
	})
	if err != nil {
		return false, err
	}

	switch status {
	case http.StatusOK:
		var out authorizeResponse
		if err := json.Unmarshal(respBody, &out); err != nil {
			return false, err
		}
		if out.Key == "" {
			return false, fmt.Errorf("authorize succeeded but returned no key")
		}
		c.mu.Lock()
		c.localKey = out.Key
		c.mu.Unlock()
		return out.Remember, nil
	case http.StatusForbidden:
		return false, ErrAuthorizeDenied
	default:
		return false, StatusError{StatusCode: status, Body: snippet(respBody)}
	}
}

// HasLocalKey reports whether the client holds a local API key for writes.
func (c *Client) HasLocalKey() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.localKey != ""
}

// SetLocalKey installs a previously obtained (remembered) local API key, so a
// caller that persisted one need not re-prompt.
func (c *Client) SetLocalKey(key string) {
	c.mu.Lock()
	c.localKey = key
	c.mu.Unlock()
}

// LocalKey returns the client's local API key, for a caller that wants to
// persist a freshly authorized one. Empty when none is held.
func (c *Client) LocalKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.localKey
}

// LibraryVersion returns the library's current clientVersion, read from the
// Last-Modified-Version header of a cheap list request. This is the value a
// single-object write passes back as If-Unmodified-Since-Version, so calling it
// just before a write also primes the Zotero-Server-ID cache.
func (c *Client) LibraryVersion(ctx context.Context, lib LibraryRef) (int, error) {
	_, page, err := c.Items(ctx, lib, ItemsOptions{Limit: 1})
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(page.LastModifiedVersion)
	if err != nil {
		return 0, fmt.Errorf("library version %q is not a number: %w", page.LastModifiedVersion, err)
	}
	return v, nil
}

// MaxWriteObjects is the most objects the local write API accepts in one batch
// (MAX_WRITE_OBJECTS upstream).
const MaxWriteObjects = 50

// WriteResult is the outcome of a batch write, mirroring the Web API v3 shape
// the local endpoint reuses: objects that were written, ones left unchanged
// because they already matched, and ones that failed, each keyed by the object's
// index in the request.
type WriteResult struct {
	Successful map[string]Envelope     `json:"successful"`
	Unchanged  map[string]string       `json:"unchanged"`
	Failed     map[string]WriteFailure `json:"failed"`
}

// WriteFailure explains why one object in a batch was rejected.
type WriteFailure struct {
	Key     string `json:"key"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Ok reports whether every object in the batch was accepted.
func (r WriteResult) Ok() bool { return len(r.Failed) == 0 }

// FirstFailure returns a representative failure, or the zero value when none.
func (r WriteResult) FirstFailure() WriteFailure {
	for _, f := range r.Failed {
		return f
	}
	return WriteFailure{}
}

func parseWriteResult(body []byte) (WriteResult, error) {
	var r WriteResult
	if err := json.Unmarshal(body, &r); err != nil {
		return WriteResult{}, fmt.Errorf("parsing write response: %w", err)
	}
	return r, nil
}

// MaxDeleteObjects is the most keys accepted in one multi-delete (upstream
// MAX_DELETE_OBJECTS).
const MaxDeleteObjects = 50

// writeBatch POSTs a batch of JSON objects to a collection route and parses the
// shared Web-v3 write response. Batch creation carries no version precondition —
// there is nothing prior to guard — which the server treats as optional here.
func (c *Client) writeBatch(ctx context.Context, path string, objs []json.RawMessage) (WriteResult, error) {
	if len(objs) == 0 {
		return WriteResult{}, nil
	}
	if len(objs) > MaxWriteObjects {
		return WriteResult{}, fmt.Errorf("%d objects exceeds the %d-object write limit", len(objs), MaxWriteObjects)
	}
	body, err := json.Marshal(objs)
	if err != nil {
		return WriteResult{}, err
	}
	status, respBody, err := c.writeRequest(ctx, http.MethodPost, path, writeOptions{body: body})
	if err != nil {
		return WriteResult{}, err
	}
	if status != http.StatusOK {
		return WriteResult{}, StatusError{StatusCode: status, Body: snippet(respBody)}
	}
	return parseWriteResult(respBody)
}

// patchObject applies a partial update to one object. Single-object writes
// require the version precondition, so ifUnmodifiedSince guards against a
// concurrent edit (412 → ErrPreconditionFailed). Success is 204.
func (c *Client) patchObject(ctx context.Context, path string, patch json.RawMessage, ifUnmodifiedSince int) error {
	status, body, err := c.writeRequest(ctx, http.MethodPatch, path, writeOptions{
		body:              patch,
		ifUnmodifiedSince: &ifUnmodifiedSince,
	})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return StatusError{StatusCode: status, Body: snippet(body)}
	}
	return nil
}

// deleteByQuery issues a version-guarded DELETE whose targets are named in the
// query string (itemKey=/collectionKey=/tag=). Success is 204.
func (c *Client) deleteByQuery(ctx context.Context, path string, q url.Values, ifUnmodifiedSince int) error {
	status, body, err := c.writeRequest(ctx, http.MethodDelete, path+"?"+q.Encode(), writeOptions{
		ifUnmodifiedSince: &ifUnmodifiedSince,
	})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return StatusError{StatusCode: status, Body: snippet(body)}
	}
	return nil
}

// CreateItems creates items from their JSON representations (itemType plus
// fields) in one batch write.
func (c *Client) CreateItems(ctx context.Context, lib LibraryRef, items []json.RawMessage) (WriteResult, error) {
	return c.writeBatch(ctx, c.profile.LibraryPrefix(lib)+"/items", items)
}

// PatchItem applies a partial update (a JSON object of fields to change) to one item.
func (c *Client) PatchItem(ctx context.Context, lib LibraryRef, key string, patch json.RawMessage, ifUnmodifiedSince int) error {
	return c.patchObject(ctx, c.profile.LibraryPrefix(lib)+"/items/"+url.PathEscape(key), patch, ifUnmodifiedSince)
}

// DeleteItems removes items by key in one request (itemKey=…).
func (c *Client) DeleteItems(ctx context.Context, lib LibraryRef, keys []string, ifUnmodifiedSince int) error {
	if len(keys) == 0 {
		return nil
	}
	if len(keys) > MaxDeleteObjects {
		return fmt.Errorf("%d keys exceeds the %d-object delete limit", len(keys), MaxDeleteObjects)
	}
	return c.deleteByQuery(ctx, c.profile.LibraryPrefix(lib)+"/items", url.Values{"itemKey": {strings.Join(keys, ",")}}, ifUnmodifiedSince)
}

// CreateCollections creates collections ({name, parentCollection}) in one batch.
func (c *Client) CreateCollections(ctx context.Context, lib LibraryRef, cols []json.RawMessage) (WriteResult, error) {
	return c.writeBatch(ctx, c.profile.LibraryPrefix(lib)+"/collections", cols)
}

// PatchCollection applies a partial update to one collection (e.g. {"name":…}).
func (c *Client) PatchCollection(ctx context.Context, lib LibraryRef, key string, patch json.RawMessage, ifUnmodifiedSince int) error {
	return c.patchObject(ctx, c.profile.LibraryPrefix(lib)+"/collections/"+url.PathEscape(key), patch, ifUnmodifiedSince)
}

// DeleteCollections removes collections by key (collectionKey=…). Deleting a
// collection does not delete its items.
func (c *Client) DeleteCollections(ctx context.Context, lib LibraryRef, keys []string, ifUnmodifiedSince int) error {
	if len(keys) == 0 {
		return nil
	}
	if len(keys) > MaxDeleteObjects {
		return fmt.Errorf("%d keys exceeds the %d-object delete limit", len(keys), MaxDeleteObjects)
	}
	return c.deleteByQuery(ctx, c.profile.LibraryPrefix(lib)+"/collections", url.Values{"collectionKey": {strings.Join(keys, ",")}}, ifUnmodifiedSince)
}

// DeleteTags removes tags from the whole library by name (tag=a||b||…),
// stripping each from every item. Names are joined with "||" as the API expects.
func (c *Client) DeleteTags(ctx context.Context, lib LibraryRef, names []string, ifUnmodifiedSince int) error {
	if len(names) == 0 {
		return nil
	}
	if len(names) > MaxDeleteObjects {
		return fmt.Errorf("%d tags exceeds the %d-tag delete limit", len(names), MaxDeleteObjects)
	}
	return c.deleteByQuery(ctx, c.profile.LibraryPrefix(lib)+"/tags", url.Values{"tag": {strings.Join(names, "||")}}, ifUnmodifiedSince)
}

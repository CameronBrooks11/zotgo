package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	if err := c.ensureServerID(ctx); err != nil {
		return false, err
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

// CreateItems creates items from their JSON representations in one batch write.
// Each element is a Zotero item object (itemType plus fields); the caller builds
// them. Creation carries no version precondition — there is nothing prior to
// guard — so this is a batch POST, which the server treats as optional there.
func (c *Client) CreateItems(ctx context.Context, lib LibraryRef, items []json.RawMessage) (WriteResult, error) {
	if len(items) == 0 {
		return WriteResult{}, nil
	}
	if len(items) > MaxWriteObjects {
		return WriteResult{}, fmt.Errorf("%d items exceeds the %d-object write limit", len(items), MaxWriteObjects)
	}
	body, err := json.Marshal(items)
	if err != nil {
		return WriteResult{}, err
	}
	status, respBody, err := c.writeRequest(ctx, http.MethodPost, c.profile.LibraryPrefix(lib)+"/items", writeOptions{body: body})
	if err != nil {
		return WriteResult{}, err
	}
	if status != http.StatusOK {
		return WriteResult{}, StatusError{StatusCode: status, Body: snippet(respBody)}
	}
	return parseWriteResult(respBody)
}

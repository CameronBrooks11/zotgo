package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxAttachmentFileSize is zotgo's explicit, conservative limit while
	// Zotero's Local API receiver buffers an upload before staging it to disk.
	MaxAttachmentFileSize   int64 = 128 * 1024 * 1024
	attachmentUploadTimeout       = 10 * time.Minute
)

var md5Pattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// AttachmentUploadMetadata describes the immutable file staged for upload.
type AttachmentUploadMetadata struct {
	MD5         string
	Filename    string
	Size        int64
	MTime       int64
	ContentType string
}

// AttachmentUploadAuthorization is Zotero's response to the file authorize phase.
type AttachmentUploadAuthorization struct {
	Exists    bool
	URL       string
	UploadKey string
}

// AuthorizeAttachmentUpload requests a new full-file upload or learns that the
// attachment already has matching bytes.
func (c *Client) AuthorizeAttachmentUpload(ctx context.Context, library LibraryRef, attachmentKey string, metadata AttachmentUploadMetadata) (AttachmentUploadAuthorization, error) {
	if err := validateAttachmentUploadMetadata(metadata); err != nil {
		return AttachmentUploadAuthorization{}, err
	}
	values := url.Values{
		"md5":      {metadata.MD5},
		"filename": {metadata.Filename},
		"filesize": {fmt.Sprintf("%d", metadata.Size)},
		"mtime":    {fmt.Sprintf("%d", metadata.MTime)},
	}
	if metadata.ContentType != "" {
		values.Set("contentType", metadata.ContentType)
	}
	status, body, err := c.attachmentFileFormRequest(ctx, library, attachmentKey, values)
	if err != nil {
		return AttachmentUploadAuthorization{}, err
	}
	if status != http.StatusOK {
		return AttachmentUploadAuthorization{}, StatusError{StatusCode: status, Body: snippet(body)}
	}
	return c.decodeAttachmentUploadAuthorization(body)
}

func (c *Client) decodeAttachmentUploadAuthorization(body []byte) (AttachmentUploadAuthorization, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return AttachmentUploadAuthorization{}, fmt.Errorf("decode attachment upload authorization: %w", err)
	}
	if fields == nil {
		return AttachmentUploadAuthorization{}, errors.New("decode attachment upload authorization: expected an object")
	}

	if rawExists, ok := fields["exists"]; ok {
		var exists int
		if json.Unmarshal(rawExists, &exists) != nil || exists != 1 {
			return AttachmentUploadAuthorization{}, errors.New("decode attachment upload authorization: invalid exists response")
		}
		for _, name := range []string{"url", "uploadKey", "prefix", "suffix"} {
			if _, present := fields[name]; present {
				return AttachmentUploadAuthorization{}, errors.New("decode attachment upload authorization: invalid exists response fields")
			}
		}
		return AttachmentUploadAuthorization{Exists: true}, nil
	}

	var response struct {
		URL       string
		UploadKey string
		Prefix    string
		Suffix    string
	}
	for name, destination := range map[string]*string{
		"url": &response.URL, "uploadKey": &response.UploadKey,
		"prefix": &response.Prefix, "suffix": &response.Suffix,
	} {
		raw, ok := fields[name]
		if !ok || json.Unmarshal(raw, destination) != nil {
			return AttachmentUploadAuthorization{}, fmt.Errorf("decode attachment upload authorization: missing or invalid %s", name)
		}
	}
	if response.URL == "" || response.UploadKey == "" {
		return AttachmentUploadAuthorization{}, errors.New("decode attachment upload authorization: missing upload URL or key")
	}
	if response.Prefix != "" || response.Suffix != "" {
		return AttachmentUploadAuthorization{}, errors.New("decode attachment upload authorization: local upload returned unexpected prefix or suffix")
	}
	if err := c.validateLocalUploadURL(response.URL, response.UploadKey); err != nil {
		return AttachmentUploadAuthorization{}, err
	}
	return AttachmentUploadAuthorization{URL: response.URL, UploadKey: response.UploadKey}, nil
}

// UploadAuthorizedAttachment streams staged bytes to Zotero's key-scoped local receiver.
func (c *Client) UploadAuthorizedAttachment(ctx context.Context, authorization AttachmentUploadAuthorization, body io.Reader, size int64) error {
	if authorization.Exists {
		return nil
	}
	if body == nil {
		return errors.New("attachment upload body is required")
	}
	if size < 0 || size > MaxAttachmentFileSize {
		return fmt.Errorf("invalid attachment upload size %d", size)
	}
	if err := c.validateLocalUploadURL(authorization.URL, authorization.UploadKey); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authorization.URL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size

	httpClient := *c.http
	if httpClient.Timeout == 0 || httpClient.Timeout < attachmentUploadTimeout {
		httpClient.Timeout = attachmentUploadTimeout
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return classifyTransport(err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode != http.StatusCreated {
		return StatusError{StatusCode: resp.StatusCode, Body: snippet(responseBody)}
	}
	return nil
}

// RegisterAttachmentUpload makes staged bytes the attachment's managed file.
func (c *Client) RegisterAttachmentUpload(ctx context.Context, library LibraryRef, attachmentKey, uploadKey string) error {
	if uploadKey == "" {
		return errors.New("attachment upload key is required")
	}
	status, body, err := c.attachmentFileFormRequest(ctx, library, attachmentKey, url.Values{"upload": {uploadKey}})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return StatusError{StatusCode: status, Body: snippet(body)}
	}
	return nil
}

// attachmentFileFormEncode emits RFC 3986-style spaces for compatibility with
// Zotero's Local API parser. Values.Encode has already escaped literal plus
// signs as %2B, so replacing its space markers cannot alter filename plus signs.
func attachmentFileFormEncode(values url.Values) string {
	return strings.ReplaceAll(values.Encode(), "+", "%20")
}

func (c *Client) attachmentFileFormRequest(ctx context.Context, library LibraryRef, attachmentKey string, values url.Values) (int, []byte, error) {
	if c.profile.Kind != EndpointLocal {
		return 0, nil, errors.New("managed attachment uploads are local-only")
	}
	if attachmentKey == "" {
		return 0, nil, errors.New("attachment key is required")
	}
	if err := c.ensureServerID(ctx); err != nil {
		return 0, nil, err
	}
	body := strings.NewReader(attachmentFileFormEncode(values))
	path := c.profile.LibraryPrefix(library) + "/items/" + url.PathEscape(attachmentKey) + "/file"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.profile.BaseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("If-None-Match", "*")
	if id := c.ServerID(); id != "" {
		req.Header.Set("Zotero-Server-ID", id)
	}
	c.mu.RLock()
	key := c.localKey
	c.mu.RUnlock()
	if key != "" {
		req.Header.Set("Zotero-API-Key", key)
	}

	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, classifyTransport(err)
	}
	defer resp.Body.Close()
	c.captureServerID(resp.Header)
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return resp.StatusCode, responseBody, ErrWriteUnauthorized
	case http.StatusPreconditionRequired:
		return resp.StatusCode, responseBody, ErrPreconditionRequired
	case http.StatusPreconditionFailed:
		return resp.StatusCode, responseBody, ErrPreconditionFailed
	}
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, responseBody, nil
}

func validateAttachmentUploadMetadata(metadata AttachmentUploadMetadata) error {
	if !md5Pattern.MatchString(metadata.MD5) {
		return errors.New("attachment MD5 must be 32 lowercase hexadecimal characters")
	}
	if metadata.Filename == "" || metadata.Filename == "." || metadata.Filename == ".." || strings.ContainsAny(metadata.Filename, `/\\`) {
		return errors.New("attachment filename must be a bare filename")
	}
	if metadata.Size < 0 || metadata.Size > MaxAttachmentFileSize {
		return fmt.Errorf("invalid attachment file size %d", metadata.Size)
	}
	if metadata.MTime < 0 {
		return fmt.Errorf("invalid attachment modification time %d", metadata.MTime)
	}
	return nil
}

func (c *Client) validateLocalUploadURL(rawURL, uploadKey string) error {
	if c.profile.Kind != EndpointLocal {
		return errors.New("managed attachment uploads are local-only")
	}
	base, err := url.Parse(c.profile.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		if err != nil {
			return fmt.Errorf("parse Local API URL: %w", err)
		}
		return errors.New("parse Local API URL: URL has no origin")
	}
	upload, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse attachment upload URL: %w", err)
	}
	expectedPath := "/api/local/uploads/" + url.PathEscape(uploadKey)
	if uploadKey == "" ||
		upload.Scheme != base.Scheme ||
		!strings.EqualFold(upload.Host, base.Host) ||
		upload.User != nil ||
		upload.RawQuery != "" || upload.ForceQuery ||
		strings.Contains(rawURL, "#") ||
		upload.EscapedPath() != expectedPath {
		return errors.New("attachment upload authorization returned an unsafe upload URL")
	}
	return nil
}

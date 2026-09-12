package zotero

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// ErrAttachmentHasNoFile reports that Zotero resolved the attachment but it has
// no stored file — a linked_url attachment, or a managed one whose bytes are
// missing locally.
var ErrAttachmentHasNoFile = errors.New("attachment has no local file")

// AttachmentLocalPath resolves one attachment to a path on this filesystem.
//
// Zotero does not stream attachment bytes: GET /items/:key/file answers 302 to a
// file:// URL, and /file/view/url returns that same URL as text/plain. So reading
// an attachment is "ask Zotero where the file is, then open that path" — which
// works only against a local Zotero, and is what CapabilityLocalFileAccess names.
//
// The returned path is Zotero's own storage location. Callers read it; nothing
// here writes to it.
func (c *Client) AttachmentLocalPath(ctx context.Context, library LibraryRef, key string) (string, error) {
	body, _, err := c.do(ctx, c.profile.LibraryPrefix(library)+"/items/"+url.PathEscape(key)+"/file/view/url", nil)
	if err != nil {
		// Zotero answers 400 when the item has no file to point at — observed as
		// `Not a file attachment: <key>`. That is an ordinary outcome for a
		// linked_url attachment, not a fault, and a caller walking an item's
		// children has to tell it apart from a real failure. 404 stays ErrNotFound:
		// a missing item is a different thing from an item without a file.
		var status StatusError
		if errors.As(err, &status) && status.StatusCode == http.StatusBadRequest {
			return "", fmt.Errorf("attachment %q: %w (%s)", key, ErrAttachmentHasNoFile, status.Body)
		}
		return "", err
	}
	return decodeAttachmentFileURL(string(body), key)
}

// decodeAttachmentFileURL turns Zotero's text/plain file URL into a filesystem
// path. It is split out so the parsing is testable without a server, and it fails
// closed: anything that is not an absolute local file:// URL is an error rather
// than a path a caller might go on to open.
func decodeAttachmentFileURL(body, key string) (string, error) {
	raw := strings.TrimSpace(body)
	if raw == "" {
		return "", fmt.Errorf("attachment %q: %w", key, ErrAttachmentHasNoFile)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("attachment %q: cannot parse file location: %w", key, err)
	}
	if parsed.Scheme != "file" {
		// A non-file scheme means Zotero answered with something that is not a
		// local file. Refuse rather than hand back a string that looks like a path.
		return "", fmt.Errorf("attachment %q: file location is %q, not a local file", key, parsed.Scheme)
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("attachment %q: file location names host %q", key, parsed.Host)
	}
	// A URL like file:some/path is opaque rather than rooted, so Path is empty.
	// That is malformed, not an absent file, and the two must not collapse into
	// the same error — callers branch on ErrAttachmentHasNoFile.
	if parsed.Opaque != "" {
		return "", fmt.Errorf("attachment %q: file location %q is not absolute", key, parsed.Opaque)
	}

	path := parsed.Path
	if path == "" {
		return "", fmt.Errorf("attachment %q: %w", key, ErrAttachmentHasNoFile)
	}
	// A Windows URL is file:///C:/Users/..., whose parsed path keeps a leading
	// slash before the drive letter. Left in place it is not a usable path.
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("attachment %q: file location %q is not absolute", key, path)
	}
	return path, nil
}

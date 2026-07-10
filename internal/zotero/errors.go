package zotero

import (
	"errors"
	"fmt"
)

var (
	// ErrZoteroDown means the endpoint refused a connection: nothing is
	// listening, so Zotero is not running.
	ErrZoteroDown = errors.New("zotero is not running")
	// ErrTransport means the request failed after a connection was established.
	// Zotero may well be running; the exchange itself broke.
	ErrTransport = errors.New("zotero request failed")
	// ErrBadPagination means Zotero advertised a rel="next" cursor that cannot
	// be followed: no start offset, an unparseable one, or one that does not
	// advance. Following it would loop forever.
	ErrBadPagination = errors.New("zotero returned an unusable pagination cursor")
	// ErrUnsupportedFormat means the requested export format is not one of the
	// Zotero translators zotgo knows how to reassemble across pages.
	ErrUnsupportedFormat = errors.New("unsupported export format")
	// ErrUnmergeableExport means the result spans several pages in a format
	// whose pages cannot be joined into one valid document.
	ErrUnmergeableExport = errors.New("export spans pages that cannot be merged")
	// ErrLocalAPIDisabled means Zotero is running, but the Local API pref is off.
	ErrLocalAPIDisabled = errors.New("zotero local api is disabled")
	// ErrNotFound means Zotero returned 404 for a requested object.
	ErrNotFound = errors.New("zotero object not found")
	// ErrLibraryNotFound means a library selector did not match My Library or a group.
	ErrLibraryNotFound = errors.New("zotero library not found")
	// ErrAmbiguousLibrary means a library selector matched more than one group name.
	ErrAmbiguousLibrary = errors.New("zotero library selector is ambiguous")
)

// StatusError preserves an unexpected non-2xx response from Zotero.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("zotero returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("zotero returned HTTP %d: %s", e.StatusCode, e.Body)
}

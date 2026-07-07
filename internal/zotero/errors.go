package zotero

import (
	"errors"
	"fmt"
)

var (
	// ErrZoteroDown means the local HTTP server could not be reached.
	ErrZoteroDown = errors.New("zotero is not running")
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

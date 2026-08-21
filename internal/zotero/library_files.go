package zotero

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// LibraryFiles reports whether one library the running Zotero can currently file
// into accepts attachment files. It answers what doctor's endpoint-level
// local-file-access capability cannot: a group library can be writable yet
// refuse files (its group has file storage disabled), and that otherwise
// surfaces only as attachments that silently never appear.
type LibraryFiles struct {
	// ID is the numeric library id (1 is My Library; a group's is its group id).
	ID int
	// Name is the library name as Zotero reports it.
	Name string
	// FilesEditable is true when the library accepts attachment files.
	FilesEditable bool
}

// LibraryFileAccess lists, per library the running Zotero can currently file
// into, whether that library accepts attachment files. It is pure diagnosis: one
// POST /connector/getSelectedCollection — the same Connector surface doctor
// already pings for liveness — with no ingestion semantics, so it does not touch
// the "connector is not a write backend" boundary. Local endpoint only.
func (c *Client) LibraryFileAccess(ctx context.Context) ([]LibraryFiles, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.profile.BaseURL+"/connector/getSelectedCollection", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Zotero-API-Version", "3")
	c.auth.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connector getSelectedCollection: HTTP %d", resp.StatusCode)
	}

	// The targets tree lists every library root and its collections; a library
	// root's id is "L<libraryID>", a collection's is its item key. Only the
	// library roots carry the account-wide file-access answer.
	var payload struct {
		Targets []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			FilesEditable bool   `json:"filesEditable"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	libraries := make([]LibraryFiles, 0, len(payload.Targets))
	for _, t := range payload.Targets {
		id, ok := libraryTargetID(t.ID)
		if !ok {
			continue // a collection target, not a library root
		}
		libraries = append(libraries, LibraryFiles{ID: id, Name: t.Name, FilesEditable: t.FilesEditable})
	}
	return libraries, nil
}

// libraryTargetID parses a Connector target id of the form "L<n>" into its
// numeric library id. Collection targets use an item key instead and yield false.
func libraryTargetID(target string) (int, bool) {
	if len(target) < 2 || target[0] != 'L' {
		return 0, false
	}
	n, err := strconv.Atoi(target[1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

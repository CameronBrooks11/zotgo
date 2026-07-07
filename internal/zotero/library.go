package zotero

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	LibraryKindUser  = "user"
	LibraryKindGroup = "group"
)

// LibraryRef is the route-level identity of a Zotero library.
type LibraryRef struct {
	Kind string
	ID   int64
	Name string
}

// UserLibrary returns the Local API route for the logged-in user's library.
func UserLibrary() LibraryRef {
	return LibraryRef{Kind: LibraryKindUser, ID: 0, Name: "My Library"}
}

// GroupLibrary returns the Local API route for a Zotero group library.
func GroupLibrary(id int64, name string) LibraryRef {
	return LibraryRef{Kind: LibraryKindGroup, ID: id, Name: name}
}

// Prefix returns the user/group path prefix used by the Local API.
func (l LibraryRef) Prefix() string {
	if l.Kind == LibraryKindGroup {
		return fmt.Sprintf("/api/groups/%d", l.ID)
	}
	return "/api/users/0"
}

// Group is the subset of /api/users/0/groups needed for library selection.
type Group struct {
	ID      int64     `json:"id"`
	Version int       `json:"version"`
	Meta    GroupMeta `json:"meta"`
	Data    GroupData `json:"data"`
}

type GroupMeta struct {
	NumItems int `json:"numItems"`
}

type GroupData struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Groups lists groups visible to the logged-in local Zotero user.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	var groups []Group
	_, err := c.getJSON(ctx, "/api/users/0/groups", nil, &groups)
	return groups, err
}

// ResolveLibrary maps a CLI-facing selector to a Local API library route.
//
// "", "me", "my", and "user" select My Library. Group selectors may be a
// numeric group id, "group/<id>", "groups/<id>", or an exact group name.
func (c *Client) ResolveLibrary(ctx context.Context, selector string) (LibraryRef, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || selector == "me" || selector == "my" || selector == "user" || selector == "users/0" {
		return UserLibrary(), nil
	}

	groupIDText := selector
	for _, prefix := range []string{"group/", "groups/"} {
		groupIDText = strings.TrimPrefix(groupIDText, prefix)
	}
	if id, err := strconv.ParseInt(groupIDText, 10, 64); err == nil {
		groups, err := c.Groups(ctx)
		if err != nil {
			return LibraryRef{}, err
		}
		for _, group := range groups {
			if group.ID == id || group.Data.ID == id {
				return GroupLibrary(id, group.Data.Name), nil
			}
		}
		return LibraryRef{}, fmt.Errorf("%w: %s", ErrLibraryNotFound, selector)
	}

	groups, err := c.Groups(ctx)
	if err != nil {
		return LibraryRef{}, err
	}
	var matches []Group
	for _, group := range groups {
		if group.Data.Name == selector {
			matches = append(matches, group)
		}
	}
	switch len(matches) {
	case 0:
		return LibraryRef{}, fmt.Errorf("%w: %s", ErrLibraryNotFound, selector)
	case 1:
		return GroupLibrary(matches[0].ID, matches[0].Data.Name), nil
	default:
		return LibraryRef{}, fmt.Errorf("%w: %s", ErrAmbiguousLibrary, selector)
	}
}

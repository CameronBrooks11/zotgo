package zotero

import (
	"context"
	"errors"
)

// WebKey is what the Web API's /keys/current reports about the API key carried
// on the request: which user it belongs to and what it may reach. It is
// meaningful only on the Web endpoint; the Local API has no such route and
// needs no key.
//
// The access grants are the endpoint's own answer to "what can this key do",
// which is why the Web profile can report capabilities honestly instead of
// guessing — unlike the local write capability, which no probe can derive.
type WebKey struct {
	UserID   int64     `json:"userID"`
	Username string    `json:"username"`
	Access   WebAccess `json:"access"`
}

// WebAccess is the key's permission grants, split into the user's own library
// and the groups it can reach.
type WebAccess struct {
	User   WebUserAccess             `json:"user"`
	Groups map[string]WebGroupAccess `json:"groups"`
}

// WebUserAccess is what the key may do in its owner's personal library.
type WebUserAccess struct {
	Library bool `json:"library"`
	Files   bool `json:"files"`
	Notes   bool `json:"notes"`
	Write   bool `json:"write"`
}

// WebGroupAccess is what the key may do in a group library.
type WebGroupAccess struct {
	Library bool `json:"library"`
	Write   bool `json:"write"`
}

// grantsRead reports whether the key can read at least one library — its owner's
// or any group it reaches. The special "all" groups entry is included.
func (k WebKey) grantsRead() bool {
	if k.Access.User.Library {
		return true
	}
	for _, g := range k.Access.Groups {
		if g.Library {
			return true
		}
	}
	return false
}

// grantsWrite reports whether the key can write to at least one library. The
// capability describes the endpoint, not what zotgo implements: v0.5 issues no
// writes, but the Web API would accept them where the key allows.
func (k WebKey) grantsWrite() bool {
	if k.Access.User.Write {
		return true
	}
	for _, g := range k.Access.Groups {
		if g.Write {
			return true
		}
	}
	return false
}

// WebKey fetches /keys/current, describing the key this Client authenticates
// with. It is only valid on the Web endpoint.
//
// A 403 or 404 means the key is missing, revoked, or wrong; both surface as
// ErrInvalidAPIKey rather than the transport-level status, so the CLI can print
// one clear message.
func (c *Client) WebKey(ctx context.Context) (WebKey, error) {
	var k WebKey
	if _, err := c.getJSON(ctx, "/keys/current", nil, &k); err != nil {
		return WebKey{}, translateKeyError(err)
	}
	return k, nil
}

// translateKeyError maps the endpoint's rejection of a key onto ErrInvalidAPIKey.
// do() interprets a bare 403 as "Local API disabled" — a local-only reading that
// is wrong for /keys/current — so both that and a plain 403/404 status collapse
// to the same key-level meaning here.
func translateKeyError(err error) error {
	if errors.Is(err, ErrLocalAPIDisabled) || errors.Is(err, ErrNotFound) {
		return ErrInvalidAPIKey
	}
	var se StatusError
	if errors.As(err, &se) && (se.StatusCode == 403 || se.StatusCode == 404) {
		return ErrInvalidAPIKey
	}
	return err
}

// selfUser is the LibraryRef for the endpoint's own user library, and the one
// place the two endpoints diverge on identity. The Local API addresses the
// logged-in user as the "0" sentinel and needs no I/O; the Web API must ask
// /keys/current for the key owner's real numeric id.
func (c *Client) selfUser(ctx context.Context) (LibraryRef, error) {
	if c.profile.Kind != EndpointWeb {
		return UserLibrary(), nil
	}
	k, err := c.WebKey(ctx)
	if err != nil {
		return LibraryRef{}, err
	}
	if k.UserID == 0 {
		return LibraryRef{}, ErrInvalidAPIKey
	}
	return LibraryRef{Kind: LibraryKindUser, ID: k.UserID, Name: "My Library"}, nil
}

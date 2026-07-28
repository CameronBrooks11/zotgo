package zotero

import "net/http"

// An Authenticator applies an endpoint's credentials to an outbound request.
//
// It is a Client collaborator rather than part of the Profile so that the
// credential never rides along with the endpoint's printable identity. The
// local endpoint needs no credential; the Web API needs an API key.
//
// The method is unexported: the two endpoints zotgo speaks to are the only
// authentication strategies there are, so there is nothing for an outside
// implementation to add.
type Authenticator interface {
	authorize(*http.Request)
}

// noAuth is the local endpoint's strategy: the Local API trusts loopback and
// asks for no credential.
type noAuth struct{}

func (noAuth) authorize(*http.Request) {}

// apiKeyAuth authenticates to the Web API with a personal API key, sent in the
// header Zotero documents as the current form (not the deprecated ?key= query
// parameter, which would leak the key into request logs and Referer headers).
type apiKeyAuth struct{ key string }

func (a apiKeyAuth) authorize(r *http.Request) {
	r.Header.Set("Zotero-API-Key", a.key)
}

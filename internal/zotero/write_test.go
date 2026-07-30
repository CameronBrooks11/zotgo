package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The client caches Zotero-Server-ID from ordinary reads so it can resend it on
// writes, which require it. A build without the header leaves the cache empty.
func TestServerID_CapturedFromReads(t *testing.T) {
	var serveHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if serveHeader {
			w.Header().Set("Zotero-Server-ID", "srv-xyz-1")
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if c.ServerID() != "" {
		t.Fatalf("fresh client ServerID = %q, want empty", c.ServerID())
	}

	// A build predating the write API sends no header: cache stays empty.
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if c.ServerID() != "" {
		t.Errorf("ServerID = %q, want empty when no header is sent", c.ServerID())
	}

	// A write-capable build sends it on every response; the client captures it.
	serveHeader = true
	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if c.ServerID() != "srv-xyz-1" {
		t.Errorf("ServerID = %q, want srv-xyz-1", c.ServerID())
	}
}

// authorizeFake mirrors the merged local write bootstrap: /api/ hands out the
// Zotero-Server-ID, and /api/local/authorize requires that header echoed back
// and an appName, then approves or denies per `approve`.
func authorizeFake(t *testing.T, approve bool) (*httptest.Server, *authorizeRecord) {
	t.Helper()
	rec := &authorizeRecord{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Zotero-Server-ID", "srv-1")
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/local/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Zotero-Server-ID", "srv-1")
		rec.gotServerID = r.Header.Get("Zotero-Server-ID")
		rec.gotAPIKey = r.Header.Get("Zotero-API-Key")
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]string
		_ = json.Unmarshal(body, &parsed)
		rec.gotAppName = parsed["appName"]
		if !approve {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"denied":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"key":"LOCALKEY123","remember":true}`))
	})
	return httptest.NewServer(mux), rec
}

type authorizeRecord struct {
	gotServerID string
	gotAPIKey   string
	gotAppName  string
}

// primeServerID gives the client a cached Zotero-Server-ID, as a real write
// client always has from a prior read (or Authorize) before it writes.
func primeServerID(c *Client) {
	h := http.Header{}
	h.Set("Zotero-Server-ID", "srv-test")
	c.captureServerID(h)
}

// A write from a fresh process (stored key, no prior read) must still bootstrap
// and send Zotero-Server-ID — writes 428 without it. Regression: previously only
// Authorize bootstrapped it, so a persisted-key write failed against real Zotero.
func TestWriteRequest_BootstrapsServerIDWhenUncached(t *testing.T) {
	var gotServerID string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Zotero-Server-ID", "srv-boot")
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		gotServerID = r.Header.Get("Zotero-Server-ID")
		_, _ = w.Write([]byte(`{"successful":{},"unchanged":{},"failed":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.SetLocalKey("K") // no prior read; Server-ID is not cached yet
	if _, err := c.CreateItems(context.Background(), UserLibrary(), []json.RawMessage{json.RawMessage(`{"itemType":"book"}`)}); err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	if gotServerID != "srv-boot" {
		t.Errorf("write sent Zotero-Server-ID %q, want srv-boot (bootstrapped via GET /api/)", gotServerID)
	}
}

func TestAuthorize_ApprovedStoresKeyAndBootstrapsServerID(t *testing.T) {
	srv, rec := authorizeFake(t, true)
	defer srv.Close()
	c := New(srv.URL)

	remember, err := c.Authorize(context.Background(), "zotgo")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !remember {
		t.Error("remember = false, want true")
	}
	if !c.HasLocalKey() {
		t.Error("key was not stored after approval")
	}
	// The bootstrap must have supplied the Server-ID the write endpoint requires.
	if rec.gotServerID != "srv-1" {
		t.Errorf("authorize sent Zotero-Server-ID %q, want srv-1", rec.gotServerID)
	}
	// Authorize is keyless — it is how the key is obtained.
	if rec.gotAPIKey != "" {
		t.Errorf("authorize sent an API key %q, want none", rec.gotAPIKey)
	}
	if rec.gotAppName != "zotgo" {
		t.Errorf("appName = %q, want zotgo", rec.gotAppName)
	}
}

func TestAuthorize_DeniedReturnsSentinel(t *testing.T) {
	srv, _ := authorizeFake(t, false)
	defer srv.Close()
	c := New(srv.URL)

	if _, err := c.Authorize(context.Background(), "zotgo"); !errors.Is(err, ErrAuthorizeDenied) {
		t.Fatalf("err = %v, want ErrAuthorizeDenied", err)
	}
	if c.HasLocalKey() {
		t.Error("a denied authorization must not store a key")
	}
}

// SetLocalKey lets a caller reuse a remembered key without re-prompting.
func TestSetLocalKey(t *testing.T) {
	c := New("http://localhost:23119")
	if c.HasLocalKey() {
		t.Fatal("fresh client should hold no key")
	}
	c.SetLocalKey("REMEMBERED")
	if !c.HasLocalKey() {
		t.Error("SetLocalKey did not install the key")
	}
}

// writeRequest maps the write-specific status codes onto the error taxonomy.
func TestWriteRequest_MapsPreconditionStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrWriteUnauthorized},
		{http.StatusPreconditionRequired, ErrPreconditionRequired},
		{http.StatusPreconditionFailed, ErrPreconditionFailed},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		c := New(srv.URL)
		primeServerID(c) // a real write client already has one; isolate the write itself
		_, _, err := c.writeRequest(context.Background(), http.MethodDelete, "/api/users/0/items/AAAA1111", writeOptions{})
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d → %v, want %v", tc.status, err, tc.want)
		}
		srv.Close()
	}
}

// The precondition header and Server-ID must ride on a write when set.
func TestWriteRequest_SendsHeaders(t *testing.T) {
	var gotVer, gotKey, gotServerID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVer = r.Header.Get("If-Unmodified-Since-Version")
		gotKey = r.Header.Get("Zotero-API-Key")
		gotServerID = r.Header.Get("Zotero-Server-ID")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.SetLocalKey("K")
	idHeader := http.Header{}
	idHeader.Set("Zotero-Server-ID", "srv-9")
	c.captureServerID(idHeader)
	ver := 42
	if _, _, err := c.writeRequest(context.Background(), http.MethodDelete, "/api/users/0/items/AAAA1111",
		writeOptions{ifUnmodifiedSince: &ver}); err != nil {
		t.Fatalf("writeRequest: %v", err)
	}
	if gotVer != "42" || gotKey != "K" || gotServerID != "srv-9" {
		t.Errorf("headers = ver:%q key:%q serverID:%q, want 42/K/srv-9", gotVer, gotKey, gotServerID)
	}
}

func TestParseWriteResult_WebV3Shape(t *testing.T) {
	body := []byte(`{
		"successful": {"0": {"key":"NEWKEY01","version":5,"data":{"key":"NEWKEY01","itemType":"book","title":"Made"}}},
		"unchanged":  {"1": "OLDKEY02"},
		"failed":     {"2": {"key":"BADKEY03","code":400,"message":"invalid field"}}
	}`)
	r, err := parseWriteResult(body)
	if err != nil {
		t.Fatalf("parseWriteResult: %v", err)
	}
	if r.Ok() {
		t.Error("Ok() true despite a failure")
	}
	if r.Successful["0"].Key != "NEWKEY01" {
		t.Errorf("successful key = %q", r.Successful["0"].Key)
	}
	if r.Unchanged["1"] != "OLDKEY02" {
		t.Errorf("unchanged = %v", r.Unchanged)
	}
	if f := r.FirstFailure(); f.Code != 400 || f.Key != "BADKEY03" {
		t.Errorf("failure = %+v", f)
	}
}

func TestCreateItems_PostsBatchAndParses(t *testing.T) {
	var gotMethod, gotKey string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotKey = r.Header.Get("Zotero-API-Key")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"successful":{"0":{"key":"NEWKEY01","version":5,"data":{"itemType":"book"}}},"unchanged":{},"failed":{}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.SetLocalKey("K")
	items := []json.RawMessage{json.RawMessage(`{"itemType":"book","title":"Made"}`)}
	r, err := c.CreateItems(context.Background(), UserLibrary(), items)
	if err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotKey != "K" {
		t.Errorf("api key = %q, want K", gotKey)
	}
	var sent []json.RawMessage
	if err := json.Unmarshal(gotBody, &sent); err != nil || len(sent) != 1 {
		t.Errorf("body not a 1-element array: %v (%s)", err, gotBody)
	}
	if !r.Ok() || r.Successful["0"].Key != "NEWKEY01" {
		t.Errorf("result = %+v", r)
	}
}

func TestCreateItems_RejectsOversizeBatch(t *testing.T) {
	items := make([]json.RawMessage, MaxWriteObjects+1)
	for i := range items {
		items[i] = json.RawMessage(`{"itemType":"book"}`)
	}
	if _, err := New("http://x").CreateItems(context.Background(), UserLibrary(), items); err == nil {
		t.Fatal("expected an over-limit error")
	}
}

func TestPatchItem_SendsPatchVersionAndSucceedsOn204(t *testing.T) {
	var gotMethod, gotVer, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVer = r.Header.Get("If-Unmodified-Since-Version")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.SetLocalKey("K")
	if err := c.PatchItem(context.Background(), UserLibrary(), "AAAA1111", json.RawMessage(`{"title":"New"}`), 12); err != nil {
		t.Fatalf("PatchItem: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/users/0/items/AAAA1111" {
		t.Errorf("%s %s, want PATCH /api/users/0/items/AAAA1111", gotMethod, gotPath)
	}
	if gotVer != "12" {
		t.Errorf("If-Unmodified-Since-Version = %q, want 12 (required for single writes)", gotVer)
	}
	if string(gotBody) != `{"title":"New"}` {
		t.Errorf("body = %s", gotBody)
	}
}

// A concurrent edit surfaces as ErrPreconditionFailed rather than silent loss.
func TestPatchItem_ConflictSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.SetLocalKey("K")
	primeServerID(c)
	err := c.PatchItem(context.Background(), UserLibrary(), "AAAA1111", json.RawMessage(`{}`), 1)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("err = %v, want ErrPreconditionFailed", err)
	}
}

func TestDeleteItems_SendsKeysAndVersion(t *testing.T) {
	var gotMethod, gotVer, gotKeys string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotVer = r.Header.Get("If-Unmodified-Since-Version")
		gotKeys = r.URL.Query().Get("itemKey")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.SetLocalKey("K")
	if err := c.DeleteItems(context.Background(), UserLibrary(), []string{"AAAA1111", "BBBB2222"}, 7); err != nil {
		t.Fatalf("DeleteItems: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotKeys != "AAAA1111,BBBB2222" {
		t.Errorf("itemKey = %q", gotKeys)
	}
	if gotVer != "7" {
		t.Errorf("version = %q, want 7", gotVer)
	}
}

func TestDeleteItems_RejectsOversize(t *testing.T) {
	keys := make([]string, MaxDeleteObjects+1)
	for i := range keys {
		keys[i] = "KEY"
	}
	if err := New("http://x").DeleteItems(context.Background(), UserLibrary(), keys, 1); err == nil {
		t.Fatal("expected an over-limit error")
	}
}

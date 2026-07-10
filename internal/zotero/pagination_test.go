package zotero

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// nextLinkServer serves an items page whose Link rel="next" header is supplied
// verbatim by the caller, so a test can hand the client a malformed cursor.
func nextLinkServer(t *testing.T, link string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", link)
		_, _ = w.Write([]byte(`[{"key":"AAAA1111","version":1,"data":{"key":"AAAA1111"}}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A cursor that never advances must be rejected, not followed forever.
func TestAllItems_RejectsNonAdvancingCursor(t *testing.T) {
	srv := nextLinkServer(t, `<http://x/api/users/0/items?start=0>; rel="next"`)

	_, err := New(srv.URL).AllItems(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrBadPagination) {
		t.Fatalf("err = %v, want ErrBadPagination", err)
	}
}

// A rel="next" URL with no start parameter is a cursor we cannot advance.
func TestAllItems_RejectsMissingStart(t *testing.T) {
	srv := nextLinkServer(t, `<http://x/api/users/0/items>; rel="next"`)

	_, err := New(srv.URL).AllItems(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrBadPagination) {
		t.Fatalf("err = %v, want ErrBadPagination", err)
	}
}

func TestAllItems_RejectsNonNumericStart(t *testing.T) {
	srv := nextLinkServer(t, `<http://x/api/users/0/items?start=abc>; rel="next"`)

	_, err := New(srv.URL).AllItems(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrBadPagination) {
		t.Fatalf("err = %v, want ErrBadPagination", err)
	}
}

// AllCollections shares the cursor logic and must reject the same way.
func TestAllCollections_RejectsNonAdvancingCursor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/collections", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<http://x/api/users/0/collections?start=0>; rel="next"`)
		_, _ = w.Write([]byte(`[{"key":"CCCC3333","version":1,"data":{"key":"CCCC3333"}}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := New(srv.URL).AllCollections(context.Background(), UserLibrary(), CollectionsOptions{})
	if !errors.Is(err, ErrBadPagination) {
		t.Fatalf("err = %v, want ErrBadPagination", err)
	}
}

// A well-formed advancing cursor still paginates to exhaustion.
func TestAllItems_FollowsAdvancingCursorToExhaustion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") {
		case "":
			w.Header().Set("Link", `<http://x/api/users/0/items?start=1>; rel="next"`)
			_, _ = w.Write([]byte(`[{"key":"AAAA1111","version":1,"data":{"key":"AAAA1111"}}]`))
		case "1":
			w.Header().Set("Link", `<http://x/api/users/0/items?start=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"key":"BBBB2222","version":1,"data":{"key":"BBBB2222"}}]`))
		default:
			_, _ = w.Write([]byte(`[{"key":"CCCC3333","version":1,"data":{"key":"CCCC3333"}}]`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, err := New(srv.URL).AllItems(context.Background(), UserLibrary(), ItemsOptions{})
	if err != nil {
		t.Fatalf("AllItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
}

func TestNextStart(t *testing.T) {
	tests := []struct {
		name     string
		nextURL  string
		cur      int
		want     int
		wantMore bool
		wantErr  bool
	}{
		{name: "no next link", nextURL: "", cur: 0},
		{name: "advancing", nextURL: "http://x/?start=25", cur: 0, want: 25, wantMore: true},
		{name: "advancing from offset", nextURL: "http://x/?start=50", cur: 25, want: 50, wantMore: true},
		{name: "repeated cursor", nextURL: "http://x/?start=25", cur: 25, wantErr: true},
		{name: "backward cursor", nextURL: "http://x/?start=10", cur: 25, wantErr: true},
		{name: "missing start", nextURL: "http://x/", cur: 0, wantErr: true},
		{name: "non-numeric start", nextURL: "http://x/?start=abc", cur: 0, wantErr: true},
		{name: "negative start", nextURL: "http://x/?start=-1", cur: 0, wantErr: true},
		{name: "unparseable url", nextURL: "://bad", cur: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, more, err := nextStart(tt.nextURL, tt.cur)
			if tt.wantErr {
				if !errors.Is(err, ErrBadPagination) {
					t.Fatalf("err = %v, want ErrBadPagination", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("nextStart: %v", err)
			}
			if got != tt.want || more != tt.wantMore {
				t.Fatalf("nextStart = (%d, %v), want (%d, %v)", got, more, tt.want, tt.wantMore)
			}
		})
	}
}

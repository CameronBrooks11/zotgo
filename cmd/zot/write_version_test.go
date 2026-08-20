package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestSingleObjectWritesUseObjectVersion(t *testing.T) {
	tests := []struct {
		name        string
		objectPath  string
		objectJSON  string
		version     int
		prepareArgs func(t *testing.T) []string
	}{
		{
			name:       "item patch",
			objectPath: "/api/users/0/items/ITEM0001",
			objectJSON: `{"key":"ITEM0001","version":7,"data":{"key":"ITEM0001","version":7,"itemType":"book","title":"Before"}}`,
			version:    7,
			prepareArgs: func(t *testing.T) []string {
				path := filepath.Join(t.TempDir(), "patch.json")
				writeTestFile(t, path, `{"title":"After"}`)
				return []string{"item", "patch", "ITEM0001", "--file", path, "--yes"}
			},
		},
		{
			name:       "item tag",
			objectPath: "/api/users/0/items/ITEM0001",
			objectJSON: `{"key":"ITEM0001","version":8,"data":{"key":"ITEM0001","version":8,"itemType":"book","title":"Tagged","tags":[]}}`,
			version:    8,
			prepareArgs: func(t *testing.T) []string {
				return []string{"tag", "add", "--item", "ITEM0001", "--yes", "reviewed"}
			},
		},
		{
			name:       "collection rename",
			objectPath: "/api/users/0/collections/COLL0001",
			objectJSON: `{"key":"COLL0001","version":9,"data":{"key":"COLL0001","version":9,"name":"Before","parentCollection":false}}`,
			version:    9,
			prepareArgs: func(t *testing.T) []string {
				return []string{"collection", "rename", "COLL0001", "After", "--yes"}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("ZOTGO_CONFIG_DIR", configDir)
			// A non-interactive write draws its key from the active lease, not a
			// standalone persisted key; seedWriteLease binds "TEST-KEY".
			seedWriteLease(t)

			var libraryVersionRequests atomic.Int32
			var gotVersion string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Zotero-Server-ID", "server-1")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == test.objectPath:
					_, _ = w.Write([]byte(test.objectJSON))
				case r.Method == http.MethodGet && r.URL.Path == "/api/users/0/items":
					libraryVersionRequests.Add(1)
					w.Header().Set("Last-Modified-Version", "99")
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPatch && r.URL.Path == test.objectPath:
					if key := r.Header.Get("Zotero-API-Key"); key != "TEST-KEY" {
						t.Errorf("Zotero-API-Key = %q, want the lease's bound key TEST-KEY", key)
					}
					gotVersion = r.Header.Get("If-Unmodified-Since-Version")
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			if _, _, err := runCLI(srv.URL, test.prepareArgs(t)...); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			if gotVersion != strconv.Itoa(test.version) {
				t.Errorf("If-Unmodified-Since-Version = %q, want %d", gotVersion, test.version)
			}
			if requests := libraryVersionRequests.Load(); requests != 0 {
				t.Errorf("made %d unnecessary library-version request(s)", requests)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

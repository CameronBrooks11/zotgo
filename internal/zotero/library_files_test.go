package zotero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// getSelectedCollectionBody mirrors a real /connector/getSelectedCollection
// response: a targets tree of library roots ("L<id>") interleaved with
// collection entries (item keys), each carrying filesEditable.
const getSelectedCollectionBody = `{
  "libraryID": 1,
  "targets": [
    {"id": "L1",       "name": "My Library",         "filesEditable": true,  "level": 0},
    {"id": "AAAA1111", "name": "A Collection",       "filesEditable": true,  "level": 1},
    {"id": "L6",       "name": "Biological Reactor", "filesEditable": false, "level": 0},
    {"id": "BBBB2222", "name": "A Group Collection", "filesEditable": false, "level": 1}
  ]
}`

func TestLibraryFileAccessParsesLibraryRoots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/connector/getSelectedCollection" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(getSelectedCollectionBody))
	}))
	defer srv.Close()

	libs, err := New(srv.URL).LibraryFileAccess(context.Background())
	if err != nil {
		t.Fatalf("LibraryFileAccess: %v", err)
	}
	// Collection targets (item-key ids) must be dropped; only library roots remain.
	want := []LibraryFiles{
		{ID: 1, Name: "My Library", FilesEditable: true},
		{ID: 6, Name: "Biological Reactor", FilesEditable: false},
	}
	if len(libs) != len(want) {
		t.Fatalf("got %d libraries, want %d: %+v", len(libs), len(want), libs)
	}
	for i, l := range libs {
		if l != want[i] {
			t.Errorf("library %d = %+v, want %+v", i, l, want[i])
		}
	}
}

func TestLibraryFileAccessErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).LibraryFileAccess(context.Background()); err == nil {
		t.Fatal("expected an error on a non-200 response, got nil")
	}
}

func TestLibraryTargetID(t *testing.T) {
	cases := []struct {
		in    string
		id    int
		isLib bool
	}{
		{"L1", 1, true},
		{"L6", 6, true},
		{"L13", 13, true},
		{"AAAA1111", 0, false}, // collection item key
		{"L", 0, false},        // malformed
		{"LX", 0, false},       // non-numeric
		{"", 0, false},
	}
	for _, c := range cases {
		id, ok := libraryTargetID(c.in)
		if ok != c.isLib || id != c.id {
			t.Errorf("libraryTargetID(%q) = (%d, %v), want (%d, %v)", c.in, id, ok, c.id, c.isLib)
		}
	}
}

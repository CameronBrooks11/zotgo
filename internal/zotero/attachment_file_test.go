package zotero

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The parser is the whole trust boundary: whatever it returns, a caller opens.
// Anything that is not an absolute local file:// URL must be an error rather than
// a string that merely looks like a path.
func TestDecodeAttachmentFileURL(t *testing.T) {
	posix := runtime.GOOS != "windows"

	cases := []struct {
		name    string
		body    string
		want    string
		wantErr string
		skip    bool
	}{
		{
			name: "posix path",
			body: "file:///home/cam/Zotero/storage/ABCD1234/paper.pdf",
			want: "/home/cam/Zotero/storage/ABCD1234/paper.pdf",
			skip: !posix,
		},
		{
			// Zotero percent-encodes spaces; a caller that skipped decoding would
			// open the wrong path or nothing at all.
			name: "percent-encoded spaces are decoded",
			body: "file:///home/cam/Zotero%20beta-test/storage/AB/Xie%20et%20al.pdf",
			want: "/home/cam/Zotero beta-test/storage/AB/Xie et al.pdf",
			skip: !posix,
		},
		{
			name: "trailing whitespace is ignored",
			body: "file:///home/cam/Zotero/storage/AB/a.pdf\n",
			want: "/home/cam/Zotero/storage/AB/a.pdf",
			skip: !posix,
		},
		{
			// file:///C:/... parses with a leading slash before the drive letter,
			// which is not a usable path on Windows.
			name: "windows drive letter loses its leading slash",
			body: "file:///C:/Users/cam/Zotero/storage/AB/a.pdf",
			want: filepath.FromSlash("C:/Users/cam/Zotero/storage/AB/a.pdf"),
			skip: posix,
		},
		{name: "empty body means no file", body: "", wantErr: "has no local file"},
		{name: "whitespace only means no file", body: "   \n", wantErr: "has no local file"},
		{name: "http is refused", body: "http://example.com/a.pdf", wantErr: "not a local file"},
		{name: "remote host is refused", body: "file://elsewhere/home/cam/a.pdf", wantErr: "names host"},
		{name: "relative path is refused", body: "file:relative/a.pdf", wantErr: "not absolute"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("platform-specific case, GOOS=%s", runtime.GOOS)
			}
			got, err := decodeAttachmentFileURL(tc.body, "ABCD1234")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

// Callers distinguish "this attachment has no file" from a transport or parse
// failure, so the sentinel has to survive wrapping.
func TestDecodeAttachmentFileURLNoFileIsASentinel(t *testing.T) {
	_, err := decodeAttachmentFileURL("", "ABCD1234")
	if !errors.Is(err, ErrAttachmentHasNoFile) {
		t.Fatalf("err = %v, want it to wrap ErrAttachmentHasNoFile", err)
	}
	if !strings.Contains(err.Error(), "ABCD1234") {
		t.Errorf("err = %v, want it to name the attachment key", err)
	}
}

// An attachment with no file is an ordinary outcome, and Zotero reports it as a
// 400 rather than an empty body — so the sentinel has to fire on the status, or
// it never fires at all and a caller walking children cannot tell "nothing to
// carry" from "something broke".
func TestAttachmentLocalPathNoFileIs400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Not a file attachment: ABCD1234"))
	}))
	defer srv.Close()

	_, err := New(srv.URL).AttachmentLocalPath(context.Background(), UserLibrary(), "ABCD1234")
	if !errors.Is(err, ErrAttachmentHasNoFile) {
		t.Fatalf("err = %v, want it to wrap ErrAttachmentHasNoFile", err)
	}
	if !strings.Contains(err.Error(), "Not a file attachment") {
		t.Errorf("err = %v, want Zotero's own reason preserved for diagnosis", err)
	}
}

// A missing item is a different condition from an item without a file, and a
// caller that conflated them would skip a genuinely broken key in silence.
func TestAttachmentLocalPathMissingItemIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := New(srv.URL).AttachmentLocalPath(context.Background(), UserLibrary(), "ZZZZZZZZ")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if errors.Is(err, ErrAttachmentHasNoFile) {
		t.Error("a missing item must not read as an attachment without a file")
	}
}

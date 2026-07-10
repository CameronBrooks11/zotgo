package zotero

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A canceled context must survive as context.Canceled, not be reported as a
// dead Zotero.
func TestContextCanceled_IsPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := New(srv.URL).Items(ctx, UserLibrary(), ItemsOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrZoteroDown) {
		t.Fatalf("cancellation misreported as ErrZoteroDown: %v", err)
	}
}

// An exceeded deadline must survive as context.DeadlineExceeded.
func TestContextDeadlineExceeded_IsPreserved(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-blocked
	}))
	defer func() { close(blocked); srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _, err := New(srv.URL).Items(ctx, UserLibrary(), ItemsOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if errors.Is(err, ErrZoteroDown) {
		t.Fatalf("deadline misreported as ErrZoteroDown: %v", err)
	}
}

// A refused connection is the one signal that genuinely means "not running".
func TestDialFailure_IsZoteroDown(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close()

	_, _, err := New(url).Items(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, ErrZoteroDown) {
		t.Fatalf("err = %v, want ErrZoteroDown", err)
	}
}

// A transport failure after the connection is established is NOT "not running".
func TestMidResponseFailure_IsTransportNotZoteroDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Accept the connection, then hang up without a valid HTTP response.
		conn.Close()
	}()

	_, _, err = New("http://"+ln.Addr().String()).Items(context.Background(), UserLibrary(), ItemsOptions{})
	if err == nil {
		t.Fatal("err = nil, want a transport error")
	}
	if errors.Is(err, ErrZoteroDown) {
		t.Fatalf("post-dial failure misreported as ErrZoteroDown: %v", err)
	}
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("err = %v, want ErrTransport", err)
	}
}

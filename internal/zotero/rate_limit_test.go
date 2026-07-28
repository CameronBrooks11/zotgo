package zotero

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sleepRecorder stands in for the client's sleep, recording each wait instead of
// blocking so rate-limit behavior is tested at full speed.
type sleepRecorder struct {
	waits []time.Duration
	err   error
}

func (s *sleepRecorder) sleep(_ context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	return s.err
}

func TestRetryAfter_ParsesSecondsAndFallsBack(t *testing.T) {
	cases := []struct {
		header string
		want   time.Duration
	}{
		{"2", 2 * time.Second},
		{"  5 ", 5 * time.Second},
		{"", defaultBackoff},
		{"garbage", defaultBackoff},
		{"-3", defaultBackoff},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.header != "" {
			h.Set("Retry-After", tc.header)
		}
		if got := retryAfter(h); got != tc.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

// A 429 with Retry-After is waited out and retried until it clears.
func TestDo_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls <= 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	rec := &sleepRecorder{}
	c.sleep = rec.sleep

	if _, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if calls != 3 {
		t.Errorf("server calls = %d, want 3 (two 429s then success)", calls)
	}
	if len(rec.waits) != 2 || rec.waits[0] != 2*time.Second || rec.waits[1] != 2*time.Second {
		t.Errorf("waits = %v, want two 2s waits", rec.waits)
	}
}

// After maxRetries, the 429 surfaces rather than looping forever.
func TestDo_GivesUpAfterMaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(srv.URL)
	rec := &sleepRecorder{}
	c.sleep = rec.sleep

	_, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{})
	var se StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want StatusError 429", err)
	}
	if len(rec.waits) != maxRetries {
		t.Errorf("waits = %d, want maxRetries=%d", len(rec.waits), maxRetries)
	}
}

// A cancelled context during a rate-limit wait aborts immediately.
func TestDo_RateLimitWaitRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.sleep = (&sleepRecorder{err: context.Canceled}).sleep

	_, _, err := c.Items(context.Background(), UserLibrary(), ItemsOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The Backoff header pauses the client between paginated requests.
func TestPagination_HonorsBackoffBetweenPages(t *testing.T) {
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/users/0/items?start=1>; rel="next"`, base))
			w.Header().Set("Backoff", "1")
			_, _ = w.Write([]byte(`[{"key":"A"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"key":"B"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	c := New(srv.URL)
	rec := &sleepRecorder{}
	c.sleep = rec.sleep

	items, err := c.AllItems(context.Background(), UserLibrary(), ItemsOptions{})
	if err != nil {
		t.Fatalf("AllItems: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("items = %d, want 2 across two pages", len(items))
	}
	if len(rec.waits) != 1 || rec.waits[0] != time.Second {
		t.Errorf("waits = %v, want one 1s backoff between pages", rec.waits)
	}
}

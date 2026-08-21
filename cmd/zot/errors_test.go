package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func TestWriteFriendlyExplainsUnknownOutcome(t *testing.T) {
	got := writeFriendly(fmt.Errorf("reading write response: %w", zotero.ErrWriteOutcomeUnknown))
	msg := got.Error()
	if !strings.Contains(msg, "may have succeeded") || !strings.Contains(msg, "duplicate") {
		t.Errorf("unknown-outcome message is not actionable: %q", msg)
	}
}

func TestFriendlyMapsTransportAndRateLimit(t *testing.T) {
	transport := friendly(fmt.Errorf("get: %w", zotero.ErrTransport)).Error()
	if !strings.Contains(transport, "lost the connection") {
		t.Errorf("transport error not made actionable: %q", transport)
	}

	for _, code := range []int{429, 503} {
		msg := friendly(zotero.StatusError{StatusCode: code}).Error()
		if !strings.Contains(msg, "rate-limiting") {
			t.Errorf("HTTP %d not surfaced as rate limiting: %q", code, msg)
		}
	}
}

func TestFriendlyPassesThroughUnmapped(t *testing.T) {
	sentinel := errors.New("some other failure")
	if friendly(sentinel) != sentinel {
		t.Error("friendly should return unmapped errors unchanged")
	}
	// A non-rate-limit StatusError is not reworded.
	other := zotero.StatusError{StatusCode: 418}
	if got := friendly(other); got.Error() != other.Error() {
		t.Errorf("friendly reworded an unmapped status error: %q", got)
	}
}

package main

import "testing"

func TestResolveVersionPrefersStamped(t *testing.T) {
	saved := version
	defer func() { version = saved }()

	version = "v1.2.3"
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want the ldflags-stamped v1.2.3", got)
	}
}

func TestResolveVersionNeverEmptyWhenUnstamped(t *testing.T) {
	saved := version
	defer func() { version = saved }()

	// Unstamped: a go-install build resolves the module version, a plain build
	// stays "dev" — either way, never empty.
	version = "dev"
	if got := resolveVersion(); got == "" {
		t.Error("resolveVersion() returned empty; want a module version or 'dev'")
	}
}

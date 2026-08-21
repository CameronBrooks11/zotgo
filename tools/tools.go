//go:build tools

// Package tools pins the versions of zotgo's build-time tools so dependabot's
// gomod ecosystem tracks and updates them — the inline `go run tool@version`
// pins the justfile used before were invisible to it (see the govulncheck and
// staticcheck breakages that motivated this).
//
// It lives in its own module, separate from the main module, on purpose:
// staticcheck v0.8.0 alone requires Go >= 1.26 to build, and folding these tools
// into the main module's go.mod would drag that requirement into the main
// dependency graph and break zotgo's own `go 1.23` compatibility floor. Isolated
// here, the tools build on a modern toolchain while the shipped binary stays a
// standard-library-plus-CLI module buildable on Go 1.23.
//
// The build tag means this file is never compiled into anything; it exists only
// so `go mod tidy` keeps the tool modules in this module's go.mod/go.sum.
package tools

import (
	_ "github.com/golangci/misspell/cmd/misspell"
	_ "golang.org/x/vuln/cmd/govulncheck"
	_ "honnef.co/go/tools/cmd/staticcheck"
)

# zotgo task runner. `just check` is the CI-equivalent gate; run it plus
# `just test` before every commit.

# List available recipes
default:
    @just --list

# Download deps, verify them, and install the local git hooks
setup:
    go mod download
    go mod verify
    git config core.hooksPath .githooks

# Format all Go code in place
fmt:
    gofmt -w .

# Fail if any Go file is not gofmt-clean
fmt-check:
    @test -z "$(gofmt -l .)" || { echo "not gofmt-clean (run 'just fmt'):"; gofmt -l .; exit 1; }

# Vet: static checks bundled with the Go toolchain. The `live` suite is behind a
# build tag, so it must be vetted explicitly or it silently rots.
lint:
    go vet ./...
    go vet -tags live ./...

# Build a version-pinned build-time tool from the tools/ module into ./bin.
# Versions live in tools/go.mod so dependabot tracks them; the separate module
# keeps their (newer) toolchain requirements out of the main 1.23 graph.
_tool name pkg:
    @go -C tools build -o "{{justfile_directory()}}/bin/{{name}}" {{pkg}}

# staticcheck: the analyses `go vet` does not carry
staticcheck: (_tool "staticcheck" "honnef.co/go/tools/cmd/staticcheck")
    ./bin/staticcheck ./...

# Catch common misspellings across tracked source, docs, and comments. Scoped
# to tracked files, so the gitignored _reference/ upstream tree is skipped.
spell: (_tool "misspell" "github.com/golangci/misspell/cmd/misspell")
    git ls-files -z | xargs -0 ./bin/misspell -error

# CI-equivalent gate: formatting, vet, staticcheck, spelling, and a full compile
check: fmt-check lint staticcheck spell
    go build ./...

# The local pre-commit gate the git hook runs (see .githooks/pre-commit)
pre-commit: check test

# Run the test suite
test:
    go test ./...

# Run the test suite under the race detector
test-race:
    go test -race ./...

# Exercise a real, running Zotero with the Local API enabled. Not run in CI:
# these tests skip themselves when Zotero is unreachable.
test-live:
    go test -tags live -count=1 -v ./... -run TestLive

# Report known vulnerabilities reachable from our code. Stdlib findings track
# the toolchain that builds them, so run this on a current Go.
vuln: (_tool "govulncheck" "golang.org/x/vuln/cmd/govulncheck")
    ./bin/govulncheck ./...

# Build the zot binary into ./bin
build:
    go build -o bin/zot ./cmd/zot

# Run zot from source (e.g. `just run doctor`)
run *args:
    go run ./cmd/zot {{args}}

# Cross-platform snapshot build via goreleaser — no publish, no system install
release-snapshot:
    go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean

# Validate the goreleaser config
release-check:
    go run github.com/goreleaser/goreleaser/v2@latest check

# zotgo task runner. `just check` is the CI-equivalent gate; run it plus
# `just test` before every commit.

# List available recipes
default:
    @just --list

# Download and verify module dependencies
setup:
    go mod download
    go mod verify

# Format all Go code in place
fmt:
    gofmt -w .

# Fail if any Go file is not gofmt-clean
fmt-check:
    @test -z "$(gofmt -l .)" || { echo "not gofmt-clean (run 'just fmt'):"; gofmt -l .; exit 1; }

# Vet: static checks bundled with the Go toolchain
lint:
    go vet ./...

# staticcheck: the analyses `go vet` does not carry
staticcheck:
    go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

# CI-equivalent gate: formatting, vet, staticcheck, and a full compile
check: fmt-check lint staticcheck
    go build ./...

# Run the test suite
test:
    go test ./...

# Run the test suite under the race detector
test-race:
    go test -race ./...

# Report known vulnerabilities reachable from our code. Stdlib findings track
# the toolchain that builds them, so run this on a current Go.
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

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

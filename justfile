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

# Download one pinned Zotero release into ./bin/zotero-<version>/ and echo the
# path to its executable. The archive format changes mid-range — 7.x ships
# .tar.bz2 and 8.0 onward ship .tar.xz — so both are tried rather than assumed.
# Versions are pinned deliberately: a floating "latest" turns an unrelated
# upstream release into a red build on somebody's PR.
_zotero version:
    #!/usr/bin/env bash
    set -euo pipefail
    dest="{{justfile_directory()}}/bin/zotero-{{version}}"
    if [ -x "$dest/zotero" ]; then echo "$dest/zotero"; exit 0; fi
    mkdir -p "$dest"
    base="https://download.zotero.org/client/release/{{version}}/Zotero-{{version}}_linux-x86_64"
    for ext in tar.xz tar.bz2; do
      if curl -fsL -o "$dest/archive.$ext" "$base.$ext" 2>/dev/null; then
        tar xf "$dest/archive.$ext" -C "$dest" --strip-components=1
        rm -f "$dest/archive.$ext"
        echo "$dest/zotero"; exit 0
      fi
    done
    echo "no Linux build published for Zotero {{version}}" >&2; exit 1

# Seed a sandbox on one pinned Zotero version and run the live suite against it.
# Headless: Zotero has no headless mode, so it runs under a virtual display.
#
# Reads work from 7.0; the local write API arrived in 10.0, so the write tests
# report as unsupported below that rather than failing.

# Seed and run the live suite against one pinned Zotero version, headless
test-live-version version port="23180":
    #!/usr/bin/env bash
    set -euo pipefail
    binary="$(just _zotero {{version}})"
    sandbox="${TMPDIR:-/tmp}/zotgo-matrix-{{version}}"
    rm -rf "$sandbox"
    # Zotero has to stay up for the tests, so the seeder cannot stop it — this
    # does, on every exit path. CI would not care; a developer running the matrix
    # locally would end up with four stray Zoteros.
    trap 'pkill -f "profile $sandbox" 2>/dev/null || true' EXIT
    xvfb-run -a --server-args="-screen 0 1280x1024x24" bash -c '
      set -euo pipefail
      ZOTERO_BIN="'"$binary"'" go run ./internal/devtool/seedsandbox \
        --unattended --wipe --port {{port}} --dir "'"$sandbox"'"
      key=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))[\"keys\"][0][\"key\"])" \
        "'"$sandbox"'/.zotero/zotero/sandbox/localAPIKeys.json" 2>/dev/null || true)
      ZOTGO_BASE_URL=http://127.0.0.1:{{port}} ZOTGO_LOCAL_KEY="$key" \
        go test -tags live -count=1 ./internal/... -run TestLive
    '

# A throwaway Zotero gets its own HOME, profile, data directory and port, so the
# live suite never runs against a real library. --wipe rebuilds from nothing.
# --unattended pre-registers an API key instead of waiting for Zotero's
# authorization modal, and is for CI, which cannot click one.
#
# (The blank line matters: just takes only the comments touching the recipe.)

# Build and seed a throwaway Zotero to develop and test against
seed-sandbox *args:
    go run ./internal/devtool/seedsandbox {{args}}

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

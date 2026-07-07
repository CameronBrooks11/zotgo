# AGENTS.md

Canonical working agreement for humans and AI agents contributing to this
repository. This is the source of truth for how to build, test, and change the
project. Tool-specific files (for example the root `CLAUDE.md`) point here.

## What this project is

`zotgo` (`zot` on the command line) is a zero-dependency Go binary and small SDK
for a running [Zotero](https://www.zotero.org/) 7+ desktop app.

- **Reads** go through Zotero's **Local API** (`/api/*`, Zotero 7+, off by
  default). Read-only, Web-API-v3 compatible.
- **Writes** are opt-in and go through Zotero's **Connector API**
  (`/connector/*`). Item creation is Zotero's responsibility, not zotgo's.
- `zotero.sqlite` is **never** opened. Talking to the app over its own HTTP
  contracts — not its database — is the reason this project exists.

It is a from-scratch successor to `pyzot`; it shares no code and carries no
attribution obligation. Licensed AGPL-3.0.

## Environment and commands

Requires [Go](https://go.dev/) 1.23+ and [`just`](https://github.com/casey/just).
The runtime dependency set is the standard library; third-party Go modules are
confined to the CLI layer and justified one at a time.

- `just setup` — download + verify modules
- `just fmt` — format (`gofmt -w`)
- `just lint` — `go vet ./...`
- `just check` — CI-equivalent gate: `gofmt` check + `go vet` + compile
- `just test` — `go test ./...`
- `just build` — build the `zot` binary into `./bin`
- `just run <args>` — run from source (e.g. `just run doctor`)
- `just release-snapshot` — cross-platform dry-run build via goreleaser

Always run `just check` and `just test` before committing. Both must be green.

## Conventions

- Commit messages: Conventional Commits (`type(scope): description`), imperative
  mood, lowercase, no trailing period. Types: `feat`, `fix`, `docs`, `refactor`,
  `test`, `chore`, `ci`, `build`, `style`. One logical change per commit.
- Code style: edit only what a change needs; do not refactor or re-comment
  untouched code. Keep the runtime dependency set at the standard library unless
  there is a clear, justified reason to add a module.
- **Never open `zotero.sqlite`.** All reads go through the Local API client and
  all writes through the Connector client, both under `internal/zotero/`.
- The SDK (`internal/zotero`) stays free of CLI concerns and third-party CLI
  deps, so it can be reused independently of the command layer.
- No cgo: builds are `CGO_ENABLED=0` static binaries.

## Layout

```text
cmd/zot/          CLI entry point (thin; command dispatch + rendering)
internal/
  zotero/         the SDK — HTTP client for the Local API + Connector API
working/          local planning docs (gitignored)
_reference/       pyzot + zotero upstream, for mining (gitignored)
```

## CI and release

Two GitHub Actions workflows live under `.github/workflows/`:

- `ci.yml` — on push to `main` and PRs: `just check`, `just test` across an
  OS matrix (ubuntu/macOS/windows × Go 1.23–1.24), and a goreleaser
  `--snapshot` build that proves the cross-platform release pipeline without
  publishing.
- `release.yml` — on a `v*.*.*` tag: goreleaser cross-compiles binaries for
  linux/macOS/windows × amd64/arm64 and publishes a GitHub Release with
  checksums.

CI runs through `just` so it matches the local gate.

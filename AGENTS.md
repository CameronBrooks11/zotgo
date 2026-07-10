# AGENTS.md

Canonical working agreement for humans and AI agents contributing to this
repository. This is the source of truth for how to build, test, and change the
project. Tool-specific files (for example the root `CLAUDE.md`) point here.

## What this project is

`zotgo` (`zot` on the command line) is a zero-dependency Go binary for a running
[Zotero](https://www.zotero.org/) 7+ desktop app.

- **Reads** go through Zotero's **Local API** (`/api/*`, Zotero 7+, off by
  default). Read-only, Web-API-v3 compatible.
- **Writes** do not exist yet. The Local API is read-only today, and zotgo waits
  for Zotero's official write contract rather than reaching around it. The
  **Connector API** (`/connector/*`) is reserved for *ingestion* — where Zotero
  performs an app-level workflow such as PDF recognition or import — and is
  never a general write backend: its save target is whatever library the user
  happens to have selected.
- `zotero.sqlite` is **never** opened. Talking to the app over its own HTTP
  contracts — not its database — is the reason this project exists.

It is a from-scratch successor to `pyzot`; it shares no code and carries no
attribution obligation. Licensed AGPL-3.0.

## Environment and commands

Requires [Go](https://go.dev/) 1.23+ and [`just`](https://github.com/casey/just).
The client (`internal/zotero`) and rendering (`internal/render`) use only the
standard library; third-party Go modules are confined to the CLI layer
(`urfave/cli/v3`) and justified one at a time.

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
- **Never open `zotero.sqlite`.** All reads go through the Local API client
  under `internal/zotero/`. If a capability is not exposed over HTTP, zotgo does
  without it rather than cracking the database.
- The client (`internal/zotero`) stays free of CLI concerns and third-party CLI
  deps, so the command layer stays a thin shell over it. It is an `internal/`
  package, not a published SDK: nothing outside this module can import it.
- No cgo: builds are `CGO_ENABLED=0` static binaries.
- **The DTOs in `internal/output` are a contract.** Renaming a field, changing
  its meaning, or removing it is a breaking change and must bump
  `output.SchemaVersion`. Adding a field is not breaking. Zotero's own envelopes
  (`internal/zotero`) are *not* a contract: they reach users only through
  `--raw`, which is explicitly unversioned. Never widen `--json` to pass a
  Zotero field through unshaped — model it as a DTO field instead.

## Layout

```text
cmd/zot/          CLI entry point (urfave/cli commands; one file per command)
internal/
  zotero/         HTTP client for Zotero's Local API (+ Connector ping)
  output/         machine-readable contract: versioned DTOs + json/jsonl/raw
  render/         human terminal output: tables and detail views
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

# AGENTS.md

Canonical working agreement for humans and AI agents contributing to this
repository. This is the source of truth for how to build, test, and change the
project. Tool-specific files (for example the root `CLAUDE.md`) point here.

## What this project is

`zotgo` (`zot` on the command line) is a zero-dependency Go binary for a running
[Zotero](https://www.zotero.org/) 7+ desktop app.

- **Reads** go through one of two endpoints, selected by profile: the **Local
  API** (`/api/*`, Zotero 7+, off by default — the default) or, under `--web`,
  the hosted **Web API** (`api.zotero.org`, API key in `ZOTGO_API_KEY`). Both
  are API-v3, so one semantic client serves both; the endpoints never silently
  fall back to each other, and each is its own version/concurrency domain.
- **Local writes** use Zotero's official local write contract (items, collections,
  and tags via `zot item / collection / tag …`), which landed upstream in
  `zotero/zotero#5015`. They need a Zotero build that has it (`zot doctor`
  reports the `write` capability); older builds and the **Web profile** stay
  read-only. Writes authorize with a local API key from a Zotero prompt and
  carry the required `Zotero-Server-ID` and `If-Unmodified-Since-Version`
  preconditions. The **Connector API** (`/connector/*`) is reserved for
  *ingestion* — app-level workflows such as PDF recognition or import — and is
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

- `just setup` — download + verify modules, install the git pre-commit hook
- `just fmt` — format (`gofmt -w`)
- `just lint` — `go vet ./...` and `go vet -tags live ./...`
- `just check` — CI-equivalent gate: `gofmt` check + vet + staticcheck + misspell
  + compile
- `just test` — `go test ./...`
- `just test-race` — the suite under the race detector
- `just test-live` — exercise a real, running Zotero (skips when it is absent)
- `just vuln` — `govulncheck`; run it on a current Go, since standard-library
  findings track the toolchain that builds them, not the code
- `just build` — build the `zot` binary into `./bin`
- `just run <args>` — run from source (e.g. `just run doctor`)
- `just release-snapshot` — cross-platform dry-run build via goreleaser

Always run `just check` and `just test` before committing. Both must be green.
`just setup` installs a pre-commit hook that runs this gate, so a normal
`git commit` enforces it; never bypass it with `--no-verify`.

### The live suite

Tests behind `//go:build live` talk to a real Zotero and never run in CI. They
exist because the `httptest` fakes are seeded from shapes *we* captured, so they
encode our reading of the API and cannot falsify it. The live tests decode
Zotero's responses independently and compare.

`just check` vets them under the build tag. Without that they compile only when
someone remembers, and an API change rots them silently.

Anything inferred from Zotero's behaviour rather than observed — how a translator
paginates, what a field means — belongs in the live suite; call the inference out
(in the PR, the changelog) until a live run confirms it.

Some live checks need seeded data or they no-op. The annotation check fails hard
when `ZOTGO_LIVE_ANNOTATED_ATTACHMENT` names an attachment (PDF/EPUB) carrying at
least a highlight and a note with distinct sort indexes; unset, it discovers or
skips. Sync that attachment so the `--web` variant sees the same key.

### `just seed-sandbox`

Builds the throwaway Zotero for you, so none of the above has to be done by hand:

```sh
just seed-sandbox              # build it, seed it, leave it running
just seed-sandbox --wipe       # rebuild from nothing
just seed-sandbox --unattended # no authorization modal; for CI
```

It creates an isolated `HOME` with its own profile, data directory and port,
starts Zotero there, and seeds a known corpus: 120 bibliographic items across
eight types, non-ASCII and braced-TeX creators, a three-level collection tree, a
PDF attachment with real bytes, two annotations covering both bodies, and related
items. It prints the `ZOTGO_BASE_URL` (and, unattended, the `ZOTGO_LOCAL_KEY`) to
run the live suite against.

Re-running is safe: it detects an already-seeded corpus and an already-running
sandbox rather than doubling either.

**It opens a Zotero window.** Zotero has no headless mode, so seeding is visible
on whatever desktop runs it — items will appear in a window as they import.

**Authorization has two paths, and the default is the honest one.** By default the
tool asks Zotero for a key through `POST /api/local/authorize` exactly as any
client would, and you approve the modal once. `--unattended` instead writes a
remembered key into the profile it just created. That is the one place anything
here reaches into Zotero's own state, and it is deliberate and narrow — the file
belongs to a profile the tool made moments earlier, it is written once and never
read back, and no database is touched. It exists because CI cannot click a modal,
and without it a version matrix is not possible at all. Prefer the default
anywhere a human is present.

**What it cannot cover.** Nine live tests still skip against it: eight need a real
`ZOTGO_API_KEY` for the Web API, and one needs a group library. Groups and web
accounts are zotero.org concepts and cannot be created locally. Everything else —
18 tests including the write round-trip — runs unattended.

### Use a throwaway profile

The live suite and any hand-run probing write to whatever library the Local API
is serving, and that is simply **whichever profile Zotero is running** — the port
is the same either way, and nothing in a response says which library you reached.
Point it at your real library and a stray write lands in your research.

So do live work in a dedicated Zotero profile with its own data directory.
`zotero --ProfileManager` opens the profile manager (`-P <name>` goes straight to
a named profile); set that profile's data directory under Settings → Advanced,
which writes `extensions.zotero.dataDir` in its `prefs.js`. The Local API toggle
(`extensions.zotero.httpServer.localAPI.enabled`) is per profile too, so enabling
it once does not enable it everywhere.

**A pref is a request; an isolated `HOME` is a boundary.** For anything scripted —
seeding, a version probe, CI — do not rely on the data-directory pref alone:

- `extensions.zotero.dataDir` is **inert on its own**. It is gated behind
  `extensions.zotero.useDataDir`, which defaults to `false`. Set only `dataDir`
  and Zotero silently ignores it, opens the default `~/Zotero` — a real library
  on most machines — and rewrites the pref file to record that it did.
- `extensions.zotero.httpServer.port` (default 23119) *is* honoured, so a test
  instance can serve its own port alongside a normal one.

Run such an instance under an overridden `HOME`, so the default-directory
fallback lands inside the sandbox rather than in someone's library. Isolated
`HOME` + `-no-remote` + an explicit `-profile` + a non-default port works, and
lets a probe run beside an ordinary Zotero without contending for the port.

Two traps follow from that:

- `profiles.ini` marks one profile `Default=1`, and a bare `zotero` opens it. That
  may not be the profile you assume. Confirm the data directory before trusting
  what you see.
- A small, collection-free, group-free library is a fact about the profile you
  happen to be on, not about Zotero. Do not generalise API behaviour from it —
  anything needing a group library cannot be observed on a personal-only profile
  at all, and should be called out as untested rather than inferred.

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
- Tests must be platform-independent — CI runs macOS and Windows. Do not assume
  Unix file permissions, or that `os.UserConfigDir` honours `XDG_*` (it does not
  off Linux). Inject paths (e.g. via an env override) and guard OS-specific
  assertions with `runtime.GOOS`.
- **The DTOs in `internal/output` are a contract.** Renaming a field, changing
  its meaning, or removing it is a breaking change and must bump
  `output.SchemaVersion`. Adding a field is not breaking. Zotero's own envelopes
  (`internal/zotero`) are *not* a contract: they reach users only through
  `--raw`, which is explicitly unversioned. Never widen `--json` to pass a
  Zotero field through unshaped — model it as a DTO field instead.
- **Versions are endpoint-scoped; the DTOs carry none.** A Zotero object version
  is only meaningful within the endpoint that issued it, and must never travel to
  another one. The Local API's version is the *server* version, so it does not
  move on unsynced local edits, and `zotero/zotero#5015` (now merged) redefines it
  as a local `clientVersion`. Do not re-add `version` to a DTO until it has a
  defined, endpoint-scoped meaning and a consumer that needs it.

## Layout

```text
cmd/zot/          CLI entry point (urfave/cli commands; one file per command)
internal/
  zotero/         HTTP client: Local API, Web API, local writes, Connector ping
  output/         machine-readable contract: versioned DTOs + json/jsonl/raw
  render/         human terminal output: tables and detail views
docs/             user + reference documentation
tools/            separate module pinning build-time tools (see below)
_reference/       pyzot + zotero upstream, for mining (gitignored)
```

`tools/` is its own Go module. It pins the versions of the build-time tools
(`staticcheck`, `govulncheck`, `misspell`) so dependabot tracks them, and stays
separate so their newer toolchain requirements never enter the main module's
`go 1.23` graph. The `just` recipes build these tools from `tools/` into `bin/`;
never add tool dependencies to the main `go.mod`.

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

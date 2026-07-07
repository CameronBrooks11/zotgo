# zotgo (`zot`)

A single, zero-dependency Go binary that drives a running
[Zotero](https://www.zotero.org/) 7+ through its own HTTP contracts — the
**Local API** (`/api/*`) for reads and the **Connector API** (`/connector/*`)
for writes. It never opens `zotero.sqlite`.

zotgo is the successor to [`pyzot`](https://github.com/CameronBrooks11/pyzot),
rebuilt from scratch to talk to Zotero the way Zotero wants to be talked to:
over HTTP, never through its database.

> **Status: early (v0.1).** A working read-only CLI — browse, search, and
> inspect your library. Export and write (`add`) commands land in subsequent
> milestones.

## How it works

zotgo requires the Zotero 7+ desktop app to be **running**. It has no
offline/SQLite mode by design — reaching into an application's database is the
architecture it exists to avoid. Reads additionally require Zotero's Local API,
which is off by default; `zot doctor` checks for it and tells you how to enable
it.

## Install

```bash
go install github.com/CameronBrooks11/zotgo/cmd/zot@latest
```

Or download a prebuilt binary for your platform from the
[Releases](https://github.com/CameronBrooks11/zotgo/releases) page — no runtime,
no dependencies, just an executable.

## Quickstart

```bash
zot doctor
```

```
zot doctor — checking Zotero at http://localhost:23119

  ✓ Zotero running  (v9.0.4)
  ✓ Local API enabled  (schema 42, API v3)

Ready. zotgo can read your library.
```

If the Local API is off, `doctor` prints the exact steps to enable it
(Zotero → Settings → Advanced → "Allow other applications on this computer to
communicate with Zotero").

## Commands

```bash
zot list                       # top-level items (default 25)
zot list -c "Smart Grid" -n 50 # items in a collection, by name or key
zot list --tag ml --tag review # items with all given tags
zot search "state estimation"  # search by title/creator/year
zot search algae --everything  # include full text and notes
zot show HRAC4E44              # one item with its attachments and notes
zot collections               # collections as a tree (--flat for a list)
zot stats                     # library-wide counts
```

Global flags: `--library/-L` selects a group library (by name or id; default is
My Library), `--json` emits JSON instead of a table, and `--url` overrides the
Zotero address. Any command can be scripted with `--json`.

## Development

Requires [Go](https://go.dev/) 1.23+ and [`just`](https://github.com/casey/just).

```bash
just check   # gofmt + go vet + compile (CI gate)
just test    # go test ./...
just run doctor
```

See [`AGENTS.md`](AGENTS.md) for the full working agreement.

## License

[AGPL-3.0](LICENSE).

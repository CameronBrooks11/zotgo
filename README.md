# zotgo (`zot`)

A single, zero-dependency Go binary that drives a running
[Zotero](https://www.zotero.org/) 7+ through its own HTTP contracts — today the
**Local API** (`/api/*`), which is read-only. It never opens `zotero.sqlite`.

zotgo is the successor to [`pyzot`](https://github.com/CameronBrooks11/pyzot),
rebuilt from scratch to talk to Zotero the way Zotero wants to be talked to:
over HTTP, never through its database.

> **Status: early (v0.2, with v0.3 on `main`).** A working read-only CLI —
> browse, search, inspect, and export your library. Writes wait on Zotero's
> official write contract; `zot doctor` reports which capabilities the endpoint
> actually offers.

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
zot doctor — checking the local endpoint at http://localhost:23119

  ✓ Zotero running  (v9.0.4)
  ✓ Local API enabled  (schema 42, API v3)

Capabilities:
  ✓ read
  ✗ write  Zotero's Local API exposes no write endpoints (upstream: zotero/zotero#5015)
  ✓ connector-ingest
  ✓ local-file-access

Ready. zotgo can read your library.
```

`doctor` reports what the endpoint can do, not merely whether it answers. Every
unsupported capability carries the reason, so there is always something to act
on — or, for `write`, something to follow. `zot --json doctor` reports the same
under `data.capabilities`, and exits non-zero when Zotero is unreachable.

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
zot export bib -c Polyhedra   # BibTeX (from Zotero), scoped to a collection
zot export csljson -o refs.json
zot export ris                # any Zotero translator: ris, biblatex, csv, …
zot export summary-md         # zotgo's own summary shapes: json, summary-csv
```

`export` hands off to Zotero's own translators — `bibtex`, `biblatex`, `csljson`,
`csv`, `mods`, `ris`, `tei`, and the `rdf_*` variants — so no bibliography
formatting is reimplemented here. zotgo shapes only `json`, `summary-csv`, and
`summary-md` itself. `-o` writes to a file instead of stdout, atomically.

`mods`, `tei`, and `rdf_*` wrap each page of results in a single XML root
element, so zotgo exports them only when the result fits in one page rather than
emitting a document with two roots; narrow the query with `-c`/`-t`.

Global flags: `--library/-L` selects a group library (by name or id; default is
My Library), and `--url` overrides the Zotero address.

## Machine-readable output

Every command speaks three mutually exclusive machine formats.

```sh
zot --json list       # one versioned document
zot --jsonl list      # one self-describing document per line
zot --raw list        # Zotero's own response, untouched
```

`--json` wraps stable zotgo DTOs in a versioned envelope. The shape is the same
for every command, so a script learns it once:

```json
{
  "schema": 2,
  "kind": "items",
  "library": { "type": "user", "id": 0, "name": "My Library" },
  "data": [ { "key": "AAAA1111", "type": "journalArticle", "title": "Algae paper" } ],
  "meta": { "shown": 25, "total": 312 }
}
```

`kind` says what `data` holds: `items`, `item`, `collections`, `collection`,
`stats`, or `health`. A `health` document carries `endpoint` and `capabilities`,
so a script can check for `write` support rather than assume it. `schema` is
bumped only when a field changes meaning or disappears — new fields may appear at
any time, so ignore the ones you don't know.

Items and collections carry **no `version`**. A Zotero object version belongs to
the endpoint that issued it, and the Local API's has no meaning zotgo can promise:
it is the *server* version, so it does not move when you edit an item locally
without syncing, and upstream
[zotero/zotero#5015](https://github.com/zotero/zotero/issues/5015) will replace it
with an unrelated local counter. Sending one to the Web API as a write
precondition is a data-integrity hazard. If you need Zotero's number anyway, take
it from `--raw`, which is explicitly outside this contract.

`--jsonl` emits one document per line, each repeating `schema`, `kind`, and
`library`. Every line therefore stands alone, and a stream survives being
truncated, split, or concatenated with another:

```sh
zot --jsonl list | jq -r '.data | "\(.key)\t\(.title)"'
```

`--raw` passes Zotero's API response straight through. It is an escape hatch for
fields zotgo does not model, and it is **not covered by `schema`**: its shape is
Zotero's and changes when Zotero changes. `stats` and `doctor` reject `--raw`,
because zotgo derives them and there is no underlying Zotero response.

`doctor` exits non-zero when Zotero is unreachable in every mode, so a script can
branch on the exit status without parsing the payload.

## Development

Requires [Go](https://go.dev/) 1.23+ and [`just`](https://github.com/casey/just).

```bash
just check      # gofmt + vet (incl. the live suite) + staticcheck + compile
just test       # go test ./...
just test-race  # the suite under the race detector
just test-live  # exercise a real, running Zotero (skips when absent)
just run doctor
```

See [`AGENTS.md`](AGENTS.md) for the full working agreement.

## License

[AGPL-3.0](LICENSE).

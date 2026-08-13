# zotgo (`zot`)

A single, zero-dependency Go binary that drives a running
[Zotero](https://www.zotero.org/) 7+ through its own HTTP contracts — browse,
search, export, and edit your library from the command line. It talks to Zotero
the way Zotero wants to be talked to: over HTTP, never through `zotero.sqlite`.

zotgo is the from-scratch successor to
[`pyzot`](https://github.com/CameronBrooks11/pyzot).

## Install

```bash
go install github.com/CameronBrooks11/zotgo/cmd/zot@latest
```

Or grab a prebuilt binary from the [Releases] page — no runtime, no dependencies.

[Releases]: https://github.com/CameronBrooks11/zotgo/releases

## Quickstart

The Zotero 7+ desktop app must be **running** with its Local API enabled;
`zot doctor` checks and, if needed, prints the steps to turn it on.

```bash
zot doctor                     # is Zotero reachable, and what can it do?
zot search "state estimation"  # find items
zot show HRAC4E44              # inspect one, with its attachments
zot export bibtex -c Polyhedra # export via Zotero's translators
zot item create < item.json    # create (needs a write-capable Zotero build)
```

Point the same commands at the hosted Web API with `--web`, and get
script-friendly output with `--json` / `--jsonl` / `--raw`.

## Documentation

The docs are published as a site at
**<https://cameronbrooks11.github.io/zotgo/>**, and the source lives in
**[`docs/`](docs/README.md)**:

- [Getting started](docs/getting-started.md) · [Reading](docs/reading.md) ·
  [Writing](docs/writing.md) · [Endpoints & profiles](docs/profiles.md) ·
  [Machine-readable output](docs/machine-output.md) ·
  [Zotero API reference](docs/zotero-api.md)

Released changes are in the [changelog](CHANGELOG.md); planned and outstanding
work is tracked in [issues](https://github.com/CameronBrooks11/zotgo/issues).

## Development

Contribution conventions, the build/test gate, and architecture notes are in
[`AGENTS.md`](AGENTS.md).

## License

[AGPL-3.0](LICENSE).

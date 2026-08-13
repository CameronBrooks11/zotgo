# zotgo documentation

`zot` is a single, zero-dependency Go binary that drives a running
[Zotero](https://www.zotero.org/) 7+ through its own HTTP contracts. It never
opens `zotero.sqlite`.

- **[Getting started](getting-started.md)** — install, enable the Local API, and `zot doctor`.
- **[Reading your library](reading.md)** — `list`, `show`, `search`, `collections`, `stats`, `export`.
- **[Writing](writing.md)** — create and edit items, collections, and tags.
- **[Endpoints & profiles](profiles.md)** — the local Zotero vs. the hosted Web API (`--web`).
- **[Machine-readable output](machine-output.md)** — `--json` / `--jsonl` / `--raw` for scripts.
- **[Zotero API reference](zotero-api.md)** — the verified HTTP contracts zotgo relies on.

Outstanding and planned work lives in the [issue tracker][issues]; released
changes are in the [changelog][changelog]. Contribution conventions are in
[`AGENTS.md`][agents].

[issues]: https://github.com/CameronBrooks11/zotgo/issues
[changelog]: https://github.com/CameronBrooks11/zotgo/blob/main/CHANGELOG.md
[agents]: https://github.com/CameronBrooks11/zotgo/blob/main/AGENTS.md

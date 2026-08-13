# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Planned and outstanding work is tracked in the
[issue tracker](https://github.com/CameronBrooks11/zotgo/issues), not here.

## [Unreleased]

### Added

- Stable JSON/JSONL mutation records for item create, patch, and delete,
  including dry runs and ordered partial outcomes.

### Fixed

- Accept the empty version some Zotero 10 beta builds return for untouched
  migrated objects, instead of rejecting the entire response
  ([zotero/zotero#6016](https://github.com/zotero/zotero/issues/6016)).
- Single-object patches (item, tag, collection) now guard on the object's own
  version, so an unrelated library change no longer rejects a valid edit with a
  version conflict.

## [0.6.0] - 2026-07-29

Local writes, against Zotero's official local write API
([zotero/zotero#5015](https://github.com/zotero/zotero/pull/5015)). Requires a
Zotero build that has that API; builds without it remain read-only.

### Added

- Item writes: `zot item create`, `patch`, `delete`, and `template` (a blank
  skeleton built from the local field endpoints).
- Collection writes: `zot collection create`, `rename`, `delete`.
- Tag writes: `zot tag add` / `remove` on an item, and `zot tag delete` to remove
  a tag from every item in the library.
- `doctor` derives the `write` capability from the `Zotero-Server-ID` header.
- Local write authorization via `/api/local/authorize`; an approved key persists
  to `~/.config/zotgo/` (override with `ZOTGO_CONFIG_DIR`).

Every write surfaces its target, previews with `--dry-run`, and confirms before
writing (`--yes` to skip). Writes carry the required `Zotero-Server-ID` and
`If-Unmodified-Since-Version` preconditions, so a concurrent edit is reported
rather than silently lost.

## [0.5.0] - 2026-07-28

The hosted Web API as an explicit, opt-in read profile.

### Added

- `--web` runs the read commands and `export` against `api.zotero.org`, with an
  API key in `ZOTGO_API_KEY`.
- Endpoint-aware `doctor` with probe-derived capabilities (from the key's own
  grants) and a `web`-shaped health report.
- Web API rate-limit handling: `Retry-After` retries on 429/503, and `Backoff`
  between paginated pages.

### Fixed

- `export csljson` over the Web API, which wraps each page as `{"items":[…]}`
  rather than the Local API's bare array; both now merge to one bare array.

## [0.3.0] - 2026-07-10

Hardened the read and transport foundation, and fixed the machine-output
contract before scripts could depend on it.

### Added

- Versioned machine-output contract: `--json`, `--jsonl`, and `--raw`, a stable
  `Document` envelope, and integer schema versioning.
- Capability-oriented `doctor` and an endpoint-profile skeleton.
- Generic Zotero-native exports over a format registry; hardened pagination and
  error handling; atomic file output; race, `govulncheck`, and `staticcheck` CI.

### Fixed

- `export csv` failing on Zotero's BOM-prefixed native CSV.

### Changed

- **Breaking:** `csv` now means Zotero's own translator; zotgo's shaped outputs
  became `summary-csv` and `summary-md`.

### Removed

- **Breaking:** the endpoint-scoped `version` field from the machine-output DTOs
  (schema 2) — an object version is meaningful only within its issuing endpoint.

## [0.2.0] - 2026-07-07

### Added

- `export` via Zotero's translators (bibtex, biblatex, csljson, ris, csv, mods,
  tei, rdf_\*) plus zotgo's own summary shapes.

## [0.1.0] - 2026-07-07

### Added

- Read commands over the Local API: `doctor`, `list`, `show`, `search`,
  `collections`, `stats`. Zero-dependency static binary and a goreleaser release
  pipeline.

[Unreleased]: https://github.com/CameronBrooks11/zotgo/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/CameronBrooks11/zotgo/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/CameronBrooks11/zotgo/compare/v0.3.0...v0.5.0
[0.3.0]: https://github.com/CameronBrooks11/zotgo/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/CameronBrooks11/zotgo/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/CameronBrooks11/zotgo/releases/tag/v0.1.0

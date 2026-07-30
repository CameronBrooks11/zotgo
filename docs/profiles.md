# Endpoints & profiles

zotgo speaks one Zotero API-v3 client, pointed at one of two endpoints by an
explicit profile. It never silently falls back from one to the other.

| Endpoint | Selected by | Auth | Notes |
|---|---|---|---|
| **Local** (default) | nothing / `--url` | none | a running desktop Zotero on this machine |
| **Web** | `--web` | `ZOTGO_API_KEY` | the hosted `api.zotero.org` |

Each endpoint is its own version and concurrency domain — an operation is never
moved between them.

## Local (default)

The desktop app must be running with its Local API enabled (see
[getting started](getting-started.md)). This is the default and the core of
zotgo: fast, offline-of-the-internet, and the only endpoint that supports writes
today.

## Web API (`--web`)

`--web` points the same read commands and `export` at the hosted Web API —
useful for headless, CI, or remote use where no desktop Zotero is running.

```bash
export ZOTGO_API_KEY=…          # from https://www.zotero.org/settings/keys
zot doctor --web                # confirm the key and see what it grants
zot --web list                  # every read command works over the Web API
zot --web export bibtex -o refs.bib
```

The API key is read only from the `ZOTGO_API_KEY` environment variable, never a
flag, so it cannot leak into your shell history or `ps` output. A read-only key is
enough — zotgo issues no writes over `--web`.

## Capabilities

`doctor` reports each endpoint's capabilities from a probe, not a guess:

- **Local**: `write` is derived from the `Zotero-Server-ID` header (present only on
  builds with the write API); `connector-ingest` and `local-file-access` need the
  desktop app.
- **Web**: `read`/`write` come from the API key's own grants; `connector-ingest`
  and `local-file-access` have no Web API equivalent and are reported unsupported.

# Getting started

## Requirements

zotgo drives the **running** Zotero 7+ desktop app over HTTP. It has no
offline/SQLite mode by design — reaching into an application's database is the
architecture it exists to avoid. Reads go through Zotero's **Local API**, which
is off by default; `zot doctor` checks for it and tells you how to turn it on.

## Install

```bash
go install github.com/CameronBrooks11/zotgo/cmd/zot@latest
```

Or download a prebuilt binary for your platform from the [Releases] page — no
runtime, no dependencies, just an executable.

[Releases]: https://github.com/CameronBrooks11/zotgo/releases

## Check your setup

```bash
zot doctor
```

```
zot doctor — checking the local endpoint at http://localhost:23119

  ✓ Zotero running  (v9.0.4)
  ✓ Local API enabled  (schema 42, API v3)

Capabilities:
  ✓ read
  ✗ write              this Zotero build has no local write API (added upstream in zotero/zotero#5015; update Zotero once a release ships it)
  ✓ connector-ingest
  ✓ local-file-access

Libraries:
  My Library          files ✓
  Biological Reactor  files ✗  attachments not accepted

Ready. zotgo can read your library.
```

`doctor` reports what the endpoint can *do*, not merely whether it answers. Every
unsupported capability carries its reason, so there is always something to act on.
`zot --json doctor` reports the same under `data.capabilities`, and exits non-zero
when Zotero is unreachable — so a script can branch on the exit status without
parsing the payload.

The **Libraries** section (local endpoint only) reports, per library you can file
into, whether it accepts attachment files. This is independent of the
endpoint-level `local-file-access` capability: a group library can be writable yet
have file storage disabled, so `local-file-access ✓` while a specific library is
`files ✗`. That is why a library can silently never gain attachments. It appears
under `data.libraries` in `--json`.

If the Local API is off, `doctor` prints the exact steps to enable it:
Zotero → Settings → Advanced → General → "Allow other applications on this
computer to communicate with Zotero".

## Next steps

- [Read your library](reading.md) — the everyday commands.
- [Write to it](writing.md) — if your Zotero build has the write API.
- [Point at a remote library](profiles.md) with `--web`.

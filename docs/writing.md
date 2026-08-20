# Writing

`zot item`, `zot collection`, and `zot tag` create and modify data on the
**local** endpoint. Writes need a Zotero build with the local write API
([zotero/zotero#5015](https://github.com/zotero/zotero/pull/5015)); older builds
are read-only, and `zot doctor` reports which you have under the `write`
capability. On a build without it, a write fails immediately with that same
explanation rather than authorizing or reaching the server — but `--dry-run`
still works, so you can validate payloads against a read-only Zotero. Writes are
refused on `--web`.

## Items

```bash
zot item template book                 # print a blank skeleton for an item type
zot item template book > b.json        # …fill it in, then:
zot item create --file b.json          # create (also reads JSON on stdin)
zot item patch KEY < patch.json        # partial update; fields you omit are untouched
zot item replace KEY < full.json       # full replace; fields you omit are reset
zot item delete KEY1 KEY2              # destructive; lists what it will remove first
```

`create` accepts a single item object or an array. `patch` takes a JSON object of
just the fields to change. `replace` takes a **whole** item object (it must
include `itemType`) and overwrites the item: any field you leave out is reset to
its default — with one exception, verified against Zotero, that an omitted `tags`
is *preserved* rather than cleared, so send `"tags": []` to strip them. Prefer
`patch` unless you specifically want that reset behaviour.

For stored and embedded Zotero-managed attachments, use `patch` only for
non-storage metadata such as `title` or `contentType`. zotgo rejects `filename`,
`path`, and `linkMode` patches because Zotero's current generic item update
changes those storage fields without moving the managed file. Generic updates
also reject a resulting managed attachment, attachment storage-mode changes,
and conversion to or from the attachment item type. Full `replace` is unavailable
when either the current or resulting attachment is managed; file renaming and
attachment conversions need a dedicated supported operation.

Item writes support stable machine output:

```bash
zot --json item create --yes --file items.json
zot --jsonl item patch KEY --yes < patch.json
zot --json item delete KEY1 KEY2 --dry-run
```

JSON always returns an `item-mutations` array; JSONL emits one self-describing
`item-mutation` document per requested item. Records preserve request order and
report `planned`, successful, missing, unchanged, or failed outcomes without
exposing raw item input or Zotero versions. Non-dry-run machine writes require
`--yes`; machine dry runs do not authorize or write. `--raw` is unavailable for
item writes because their result is a zotgo-derived mutation report, not one raw
Zotero response.

## Managed attachments

```bash
zot attachment import \
  --parent ITEMKEY \
  --file figure.png \
  --title "Figure 1" \
  --source-url https://example.org/figure.png
```

`attachment import` attaches one non-empty regular file to an existing
bibliographic parent. It copies a stable snapshot into private staging, creates
`imported_file` metadata, uploads those exact bytes to Zotero, registers the
managed file, and verifies the parent, title, source URL, filename, media type,
MD5, and byte length. `--source-url` stores provenance only; zotgo never
downloads it.

The media type comes from the first 512 staged bytes, not the extension.
`--content-type` supplies a concrete MIME override, and `--filename` changes the
managed filename. Imports are capped at 128 MiB because Zotero's current Local
API receiver buffers each upload before staging it. Standard input, directories,
and special files are not accepted.

Before writing, zotgo checks every direct attachment child. An exact MD5 match
is a successful no-op unless `--allow-duplicate` is set. The check is best-effort
rather than atomic because Zotero has no create-unless-this-parent-has-no-
matching-checksum precondition.

Import has separate metadata, authorization, byte-upload, registration, and
verification phases. If a phase after metadata creation fails, stable output
reports the attachment key, last completed stage, and a bounded failure without
deleting anything automatically. `--raw` is unavailable because no single
Zotero response represents the operation.

Generic `item create` does not ingest local bytes. For a new `imported_file`
item it rejects local `path` and premature `filename` fields and directs the user
to `attachment import`; metadata-only creates produce a warning.

## Collections

```bash
zot collection create "Smart Grid" -p PARENTKEY   # -p/--parent is optional
zot collection rename KEY "New Name"
zot collection move KEY --to PARENTKEY            # reparent; --to-top moves to the top level
zot collection delete KEY                          # the collection's items are kept
```

Collection writes support the same machine output as items: `--json` returns a
`collection-mutations` array, `--jsonl` one `collection-mutation` per line, with
statuses `planned`, `created`, `renamed`, `moved`, `deleted`, `notFound`, and
`failed`.

## Tags

```bash
zot tag add urgent todo --item ITEMKEY   # add tags to one item
zot tag remove todo --item ITEMKEY       # remove tags from one item
zot tag delete urgent                    # remove a tag from EVERY item (library-wide)
```

`tag add`/`remove` edit one item's tags and preserve the rest; `tag delete`
strips a tag from the whole library.

Tag writes emit `tag-mutations` (`--json`) or `tag-mutation` lines (`--jsonl`),
one record per requested tag. `add`/`remove` carry the target `item` and report
`added`, `removed`, or `unchanged` (a tag already in the desired state triggers
no write); `delete` reports `planned` then `deleted` and omits `item`.

## Safety and authorization

Every write **surfaces the target library, shows what it will do, and asks to
confirm**:

- `--dry-run` previews without writing (and without authorizing).
- `--yes` skips the confirmation prompt for scripts.

There are two ways a write is authorized, matched to who is driving it.

**Interactive writes** — a human at a terminal answering the confirmation prompt —
are their own authority. The first such write prompts for approval in Zotero.
Choosing **Always Allow** stores a local API key in `~/.config/zotgo/local-api-key`
(mode `0600`) so later interactive writes don't re-prompt; **Allow** grants a
single-use key. Interactive `attachment import` requires **Always Allow**, because
it performs several authenticated phases and a single-use key would be spent on the
first.

**Non-interactive writes** — anything with `--yes`, or machine output
(`--json`/`--jsonl`) — require a **write lease**, so an agent's blast radius is
bounded and auditable instead of the whole library. A lease is a human-granted,
time-boxed, scoped capability; without one, a non-interactive write fails closed
with `run 'zot grant'`.

```bash
zot grant                                   # 30-min lease, all non-destructive ops, My Library
zot grant --ttl 2h -o item.patch -o tag.add # narrower scope and lifetime (max 24h)
zot grant --note "cleanup for project X"    # a note recorded in the lease and audit log
zot grant status                            # show the active lease and its audit summary
zot grant revoke                            # end it early
```

`zot grant` is deliberately the inverse of every other write command: it **must**
run in an interactive terminal (a human approves Zotero's authorize modal and the
printed scope), so an agent cannot mint its own lease and `--yes` does not apply.
The lease carries the write key it authorizes, so expiry and `revoke` actually
remove write ability. Every decision — allowed or refused — is appended to the
lease's audit log (`~/.config/zotgo/audit/<id>.jsonl`), summarized by
`zot grant status`. `--dry-run` still previews non-interactive writes without a
lease. Set `ZOTGO_CONFIG_DIR` to relocate the config directory.

The lease **contains a rule-following agent** — one that only invokes `zot` and
honors refusals — and gives you an audit trail. It is **not** a sandbox: on a
single-user machine any process running as you could write the lease file, read a
stored key, or call Zotero's local API directly, so the lease bounds *accidental*
blast radius, not a determined one. Revoking a lease removes zotgo's state only; if
you clicked **Always Allow**, revoke that key in Zotero's settings too, since the
Local API has no remote revoke. See
[the design doc](design/write-authority.md) for the full model and threat
boundaries.

Writes carry Zotero's required `Zotero-Server-ID` and operation-specific
preconditions, so concurrent or mismatched writes are rejected and reported
rather than silently overwriting.

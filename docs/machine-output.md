# Machine-readable output

Every command speaks three mutually exclusive machine formats.

```sh
zot --json list       # one versioned document
zot --jsonl list      # one self-describing document per line
zot --raw list        # Zotero's own response, untouched
```

## `--json`

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

`kind` says what `data` holds: `items`, `item`, `attachment`,
`attachment-import`, `note`, `relations`, `relation`, `collections`,
`collection`, `stats`, `health`, or one of the mutation kinds (`item-mutations`,
`collection-mutations`, `tag-mutations`, and their singular `*-mutation` forms
under `--jsonl`). A `health` document carries `endpoint` and `capabilities`, so a
script can check for `write` support rather than assume it. `schema` is bumped
only when a field changes meaning or disappears — new fields may appear at any
time, so ignore the ones you don't know.

### Writes

Every write command emits a mutation document whose `data` is always an array,
even for a single object. Each record carries the request `index`, its
`operation`, and a `status`; dry runs use `planned` and perform no authorization
or write. Records preserve request order, and a partial batch emits every
outcome before the command exits with status 1. Mutation documents have no
pagination `meta` and never expose Zotero object versions or the raw request
body.

- **Items** — `zot item create`, `patch`, `replace`, `delete` →
  `item-mutations`. Statuses: `planned`, `created`, `unchanged`, `patched`,
  `replaced`, `deleted`, `notFound`, `failed`. Context fields `key`, `type`,
  `title`, and sorted patch `fields` appear when known; a failed create carries a
  structured `failure` with `code` and `message`.
- **Collections** — `zot collection create`, `rename`, `move`, `delete` →
  `collection-mutations`. Statuses: `planned`, `created`, `renamed`, `moved`,
  `deleted`, `notFound`, `unchanged`, `failed`. Records carry `key`, `name`, and
  `parentKey` when known.
- **Tags** — `zot tag add`, `remove`, `delete` → `tag-mutations`. Each requested
  tag is one record with its `tag` name; `add`/`remove` also carry the target
  `item`, while the library-wide `delete` omits it. Statuses: `planned`, `added`,
  `removed`, `deleted`, `unchanged`. A tag already in the desired state is
  reported `unchanged` and triggers no write.

Non-dry-run machine writes require `--yes` with `--json` or `--jsonl`, so an
automation command never falls back to an interactive prompt.

### Show

`zot --json show ITEM_KEY` and `--jsonl` emit one stable `item` document. The
shaped item is `.data`; all of its shaped direct children are
`.data.children`, in Zotero's page order.

`zot --raw show ITEM_KEY` composes multiple Zotero responses as
`{"item": <envelope>, "children": [<envelope>, ...]}`. The wrapper and array
separators are synthesized by zotgo, while every embedded item envelope retains
Zotero's fields and scalar representations. Raw `show` validates and buffers all
pages before writing, so a malformed or failed later page produces no partial
stdout.

### Relations

`zot --json relation list ITEM_KEY` emits a `relations` document in stable
predicate/target order; JSONL emits one `relation` document per edge. Every
record includes the source `itemKey`, predicate, and complete target URI. The
target URI is authoritative. `targetKey` is only a convenience and appears when
the target is a strict Zotero user, local-user, or group item URI.

`--raw` emits the complete source item envelope after validating its identity.
It does not shape the `relations` field, so malformed or future relation data
remains available through the raw escape hatch.

### Collection paths

`zot --json collection path KEY...` emits a `collections` document in requested
key order. Each ordinary collection record gains a `path` array of `{key,name}`
segments from root to leaf; JSONL emits the same records individually with
`kind: "collection"`. The segment array is the stable ancestry contract. Joined
names shown to humans are presentation only, since collection names can contain
path-like punctuation.

The command fetches the complete collection index once and follows normal API
pagination and backoff. It rejects `--raw` before making a request because a
resolved path is derived from multiple collection records rather than one Zotero
response.

### Attachments

`zot --json attachment show ATTACHMENT_KEY` emits one `attachment` record.
Its bounded fields cover identity and parent, title and link mode, content and
filename metadata, URL and dates, tags, nullable `md5`/`mtime`, and a nullable
`enclosure`. JSONL emits the same record on one self-describing line. An
enclosure carries the content type and optional size Zotero advertised for the
file; its download href is endpoint-scoped (it differs between the Local and Web
endpoints for the same attachment), so it is deliberately excluded from the
stable record — reach for `--raw` when you need it. None of the enclosure is a
portable filesystem-existence assertion.

`--raw` emits Zotero's complete single-item envelope after validating only the
response key and item type; changes to other Zotero-owned field shapes do not
block the raw escape hatch.

### Attachment import

`zot --json attachment import --parent KEY --file PATH --yes` emits one
`attachment-import` record. It reports `planned`, `duplicate`, `imported`,
`partial`, or `failed`, the last completed stage, the nullable created key, the
staged filename/type/size/MD5, focused verification, and a bounded failure when
needed. `verification.actualFilename` is the filename read back from Zotero, so
scripts can distinguish the requested name from the stored result. Nullable
fields remain present as `null` in planned and partial records.

Import is a derived multi-response mutation, so JSONL emits the same single
self-describing record and `--raw` is unavailable. A failure after attachment
metadata exists emits the partial record before exiting with status 1; it never
silently rolls back the created item. Like other writes, a non-interactive import
requires a write lease permitting `attachment.import`.

### Notes

`zot --json note show NOTE_KEY` emits one `note` record containing its key,
parent key, dates, tags, and Zotero's exact rich-text HTML string. An empty note
has `"html": ""`; standalone notes have `"parentKey": ""`. JSONL emits the
same record on one self-describing line. The command never derives plain text or
rewrites the HTML.

`--raw` emits the complete single-item envelope after key and item-type
validation, even when fields outside that identity cannot be shaped as a stable
note record.

### No `version` field

Items, collections, attachment, and note records carry **no `version`**. A Zotero
object version belongs to the endpoint that issued it, and the Local API's has
no meaning zotgo can promise:
it is the *server* version, so it does not move when you edit an item locally
without syncing, and the local write API replaces it with an unrelated local
counter. Sending one to the Web API as a write precondition is a data-integrity
hazard. If you need Zotero's number anyway, take it from `--raw`, which is
explicitly outside this contract.

## `--jsonl`

`--jsonl` emits one document per line, each repeating `schema`, `kind`, and
`library`. Every line therefore stands alone, and a stream survives being
truncated, split, or concatenated with another. Write records use the singular
kind (`item-mutation`, `collection-mutation`, `tag-mutation`), one per line:

```sh
zot --jsonl list | jq -r '.data | "\(.key)\t\(.title)"'
```

## `--raw`

`--raw` passes Zotero's API response straight through. It is an escape hatch for
fields zotgo does not model, and it is **not covered by `schema`**: its shape is
Zotero's and changes when Zotero changes. Commands backed by multiple requests,
such as `show`, may place complete Zotero envelopes inside a documented synthetic
wrapper. `stats`, `doctor`, and every write command (item, collection, and tag)
reject `--raw`, because their output is derived and is not a raw Zotero response.
Managed attachment import also rejects `--raw` because its result combines
metadata creation, upload, registration, and verification.

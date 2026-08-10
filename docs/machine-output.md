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

`kind` says what `data` holds: `items`, `item`, `collections`, `collection`,
`stats`, `health`, or `item-mutations`. A `health` document carries `endpoint`
and `capabilities`, so a script can check for `write` support rather than assume
it. `schema` is bumped only when a field changes meaning or disappears — new
fields may appear at any time, so ignore the ones you don't know.

### Item writes

`zot item create`, `patch`, and `delete` emit an `item-mutations` document whose
`data` is always an array, including a single create or patch. Every record has
the request `index`, its `operation`, and a status such as `planned`, `created`,
`unchanged`, `failed`, `patched`, `deleted`, or `notFound`. Context fields (`key`,
`type`, `title`, and sorted patch `fields`) appear when known; failed creates
carry a structured `failure` with `code` and `message`. Dry runs use `planned`
and perform no authorization or write.

Non-dry-run item writes require `--yes` with `--json` or `--jsonl`. This prevents
an automation command from falling back to an interactive prompt. A partial
create emits all per-item outcomes in request order, then exits with status 1.
Mutation documents have no pagination `meta` and never expose Zotero object
versions or the raw request body.

### No `version` field

Items and collections carry **no `version`**. A Zotero object version belongs to
the endpoint that issued it, and the Local API's has no meaning zotgo can promise:
it is the *server* version, so it does not move when you edit an item locally
without syncing, and the local write API replaces it with an unrelated local
counter. Sending one to the Web API as a write precondition is a data-integrity
hazard. If you need Zotero's number anyway, take it from `--raw`, which is
explicitly outside this contract.

## `--jsonl`

`--jsonl` emits one document per line, each repeating `schema`, `kind`, and
`library`. Every line therefore stands alone, and a stream survives being
truncated, split, or concatenated with another. Item write records use the
singular kind `item-mutation`:

```sh
zot --jsonl list | jq -r '.data | "\(.key)\t\(.title)"'
```

## `--raw`

`--raw` passes Zotero's API response straight through. It is an escape hatch for
fields zotgo does not model, and it is **not covered by `schema`**: its shape is
Zotero's and changes when Zotero changes. `stats`, `doctor`, and the three item
write commands reject `--raw`, because their output is derived and is not a raw
Zotero response.

# Writing

`zot item`, `zot collection`, and `zot tag` create and modify data on the
**local** endpoint. Writes need a Zotero build with the local write API
([zotero/zotero#5015](https://github.com/zotero/zotero/pull/5015)); older builds
are read-only, and `zot doctor` reports which you have under the `write`
capability. Writes are refused on `--web`.

## Items

```bash
zot item template book                 # print a blank skeleton for an item type
zot item template book > b.json        # …fill it in, then:
zot item create --file b.json          # create (also reads JSON on stdin)
zot item patch KEY < patch.json        # partial update; fields you omit are untouched
zot item delete KEY1 KEY2              # destructive; lists what it will remove first
```

`create` accepts a single item object or an array. `patch` takes a JSON object of
just the fields to change.

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

## Collections

```bash
zot collection create "Smart Grid" -p PARENTKEY   # -p/--parent is optional
zot collection rename KEY "New Name"
zot collection delete KEY                          # the collection's items are kept
```

## Tags

```bash
zot tag add urgent todo --item ITEMKEY   # add tags to one item
zot tag remove todo --item ITEMKEY       # remove tags from one item
zot tag delete urgent                    # remove a tag from EVERY item (library-wide)
```

`tag add`/`remove` edit one item's tags and preserve the rest; `tag delete`
strips a tag from the whole library.

## Safety and authorization

Every write **surfaces the target library, shows what it will do, and asks to
confirm**:

- `--dry-run` previews without writing (and without authorizing).
- `--yes` skips the confirmation prompt for scripts.

The first write prompts for approval in Zotero. Choosing **Always Allow** stores a
local API key in `~/.config/zotgo/local-api-key` (mode `0600`) so later writes
don't re-prompt; **Allow** grants a single-use key. Set `ZOTGO_CONFIG_DIR` to
relocate the config directory.

Writes carry Zotero's required `Zotero-Server-ID` and `If-Unmodified-Since-Version`
preconditions, so if the library changed since the command read it, the write is
rejected and reported rather than silently overwriting.

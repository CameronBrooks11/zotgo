# Zotero integration contracts (verified 2026-07-07)

zotgo talks to a **running Zotero 7+ desktop app** over two HTTP surfaces on
`localhost:23119`. It never touches `zotero.sqlite`. This file is the verified
reference for both surfaces; everything below was captured against a live
Zotero (client `9.0.4`, Local API v3, schema 42, `~8784047` user, 13 groups,
2203 items in My Library).

The two surfaces:

| Surface | Path prefix | Purpose | Default state |
|---|---|---|---|
| **Local API** (read) | `/api/*` | read library data, server-side export | **OFF by default** (`httpServer.localAPI.enabled`) |
| **Connector API** (write) | `/connector/*` | create items/attachments, snapshots | on whenever Zotero runs |

Both require Zotero to be running. This is the central constraint: **zotgo has
no headless/offline mode**.

---

## 1. Local API (read) — `/api/*`

Web-API-v3-compatible. Same JSON envelope and headers as `api.zotero.org`, so
the online API docs (`_reference/zotero-upstream/zotero-docs/content/dev/web_api/`)
apply. Served by `server_localAPI.js`.

### Availability / probe

- `GET /api/` → `200 "Nothing to see here."` when reachable. This no-op is the
  version-agnostic probe (it ignores API-version mismatch).
- When the pref is **off**, every `/api/*` route except `/api/` returns
  `403 text/plain "Local API is not enabled"`. zotgo must detect this exact
  case and tell the user how to enable it (Settings → Advanced → "Allow other
  applications on this computer to communicate with Zotero" + Local API).
- Connection refused → Zotero not running.

### Response envelope

Every item/collection object:

```json
{
  "key": "HRAC4E44",
  "version": 3579,
  "library": { "type": "user", "id": 8784047, "name": "My Library", "links": {…} },
  "links":   { "self": {…}, "alternate": {…}, "attachment": { "href": ".../items/CWEW5DNC", "attachmentType": "application/pdf", "attachmentSize": 585858 } },
  "meta":    { "creatorSummary": "Posten", "parsedDate": "2009-06", "numChildren": 1 },
  "data":    { "key": "…", "version": 3579, "itemType": "journalArticle", "title": "…", "creators": [...], "tags": [...], "collections": [...] }
}
```

- `data` is polymorphic by `itemType`. Decode as a typed envelope with
  `Data json.RawMessage`; unmarshal fields on demand.
- `meta` gives server-derived fields pyzot rebuilt by hand from SQL:
  `creatorSummary`, `parsedDate`, `numChildren`. Free.
- `links.attachment` inlines the primary attachment (type + size) on a
  top-level item — no extra round trip to know it has a PDF.

### Response headers (pagination + versioning)

```
Total-Results: 2203
Last-Modified-Version: 3579
Link: <…?limit=1&start=1>; rel="next", <…?start=2202>; rel="last"
Zotero-Schema-Version: 42
```

- **Pagination**: follow `Link rel="next"` or page with `start`/`limit`.
- **Counts are free**: `GET …?limit=1` and read `Total-Results` (this is how
  `zot stats` should count without pulling rows).
- `Last-Modified-Version` enables cheap change-detection / caching later.

### Route map (from `server_localAPI.js`)

Item routes exist under **both** `/api/users/:userID/…` and `/api/groups/:groupID/…`:

```
/api/users/:userID/items                      list all items
/api/users/:userID/items/top                  top-level only (parents)
/api/users/:userID/items/trash                trashed
/api/users/:userID/items/tags                 tags in item set
/api/users/:userID/items/:itemKey             one item
/api/users/:userID/items/:itemKey/children    attachments + notes + annotations
/api/users/:userID/items/:itemKey/file        302 redirect to the local file URL
/api/users/:userID/items/:itemKey/file/view/url  local file URL
/api/users/:userID/items/:itemKey/fulltext    full-text content (per item)
/api/users/:userID/collections[/top]          collections (tree via parentCollection)
/api/users/:userID/collections/:key           one collection
/api/users/:userID/collections/:key/items[/top]  items in a collection
/api/users/:userID/collections/:key/collections  subcollections
/api/users/:userID/searches/:searchKey/items  saved-search results
/api/users/:userID/tags                       all tags
/api/users/:userID/groups                     groups (list)
/api/groups/:groupID                          one group
```

Schema surface (no library id needed):

```
/api/                 no-op probe / version
/api/schema           full Zotero schema (200)
/api/itemTypes        [{itemType, localized}, …]
/api/itemFields
/api/itemTypeFields
/api/itemTypeCreatorTypes
/api/creatorFields
```

### Query parameters

- `limit`, `start` — pagination.
- `q` + `qmode=titleCreatorYear|everything` — search (verified: `q=algae&itemType=journalArticle` → 3; `q=photobioreactor&qmode=everything` → 2).
- `itemType=journalArticle` (supports `||`/`-` boolean per web API).
- `itemKey=KEY1,KEY2` — restrict to specific keys (verified live 2026-07-10).
  On `/items` Zotero *also* returns the matched items' children; on `/items/top`
  it returns exactly the requested keys. An empty `itemKey=` is ignored, not
  treated as "match nothing".
- `format=json|bibtex|csljson|ris|biblatex|mods|tei|rdf_*|csv|…` — **server-side
  export via Zotero's translators** (all verified 200 live: bibtex, csljson, ris,
  biblatex, mods, tei, rdf_bibliontology, Zotero-native csv). Expose these
  generically through one `format=` passthrough, not one method per format. Note
  the Zotero-native `csv` differs from zotgo's hand-rolled summary csv.
  `include=bib,citation` and `style=` also apply.
- `sort`, `direction`.

### Export page-boundary behavior (verified live 2026-07-10, Zotero 9.0.4)

Confirmed against a real 1093-item library by forcing `limit=1` over an
`itemKey=`-scoped query. Previously inferred from document structure only.

- **bibtex / biblatex / ris** — one record per entry; pages concatenate cleanly.
- **csljson** — each page is a JSON array; they splice into one array.
- **Zotero-native `csv`** — the *full* header row **is** repeated on every page,
  so header-dedupe is correct. **Each page is also prefixed with a UTF-8 BOM**
  (`EF BB BF`). Go's `encoding/csv` does not treat the BOM specially: it becomes
  part of the first field, and the quote that follows reads as a bare quote in an
  unquoted field, so parsing fails outright. Strip the BOM per page and re-emit
  one on the merged document (Zotero emits it so spreadsheets detect UTF-8).
  A BOM may also legitimately appear *inside* a field value — only strip the
  leading one.
- **mods / tei / rdf_\*** — each page is a complete document wrapping its records
  in a single root (`<modsCollection>`, `<listBibl>`, `<rdf:RDF>`); mods and tei
  additionally emit an `<?xml?>` declaration per page. Concatenation is therefore
  never valid; multi-page requests must be refused.
- Zotero's CSV translator **skips standalone attachments**: a library with 1093
  top-level items (19 of them standalone attachments) exports 1074 CSV rows.
  That omission is Zotero's, not a merge defect.

### The `/file` endpoint = a storage-path simplification, not byte streaming

`GET /items/:key/file` returns a `302` redirect to the attachment's local
`file://` URL; `GET /items/:key/file/view/url` returns the same local file URL
as `text/plain`. zotgo does **not** need to know the Zotero data-dir /
`storage/<key>/` layout that pyzot resolved by hand, but attachment access is
"ask Zotero for the file URL, then open/read that path" rather than a plain HTTP
byte stream. Do not assume a normal HTTP client following redirects will fetch
the bytes correctly.

### Library-id / user-vs-group routing (correctness-critical)

- `userID = 0` is accepted and resolved to the logged-in user (responses report
  the real id, e.g. `8784047`). Groups use their real numeric id.
- `GET /api/users/0/groups` → `[{id, version, meta:{numItems}, data:{id,name,description}}]`.
- **Restriction (from source):** *"No access to user data for users other than
  the local logged-in user."* Single-user only; you cannot read another user's
  personal library.
- zotgo must map its notion of "which library" → either `users/<uid>` or
  `groups/<gid>`. Getting this wrong reads the wrong library silently. This is
  the #1 correctness risk carried over from pyzot's issue-4 analysis.

---

### Local write contract (released in Zotero 10.0)

`zotero/zotero#5015` (commits 9dd17a2, 77f2432, a37a9e7; dstillman finished
AbeJellinek's branch) added the local write API, and it ships in **Zotero 10.0**.
zotgo's write support is live-verified against that build. Writes activate only
when the running Zotero exposes the write endpoints (the `Zotero-Server-ID`
header is the probe signal); on an older build, write commands fail fast with the
same explanation `doctor` gives, and `--dry-run` still works.

**Endpoints & methods** (mirror Web API v3 write semantics):
- `POST /api/local/authorize` — obtain a **local API key** (local-only; no web
  analog). Body `{"appName":"zotgo"}` → Zotero modal (Allow / Always Allow /
  Deny). 200 `{"key":"<key>","remember":<bool>}`; 403 `{"denied":true}`; 400 if
  appName blank; 429 + `Retry-After` if rate-limited. "Allow" keys are
  **single-use** (consumed by the first successful write) → always handle 401 by
  re-authorizing.
- Batch create/delete on the collection routes: `POST`/`DELETE`
  `/…/items`, `/…/collections` (`DELETE` multi-key via query param, ≤50).
- Per-object: `PUT`/`PATCH`/`DELETE` `/…/items/:key`, `/…/collections/:key`.
- `MAX_WRITE_OBJECTS = 50`, `MAX_DELETE_OBJECTS = 50`.

**Auth & headers:**
- Writes require the local API key via `Zotero-API-Key` (or `?key=`); missing/bad
  → 401.
- **`Zotero-Server-ID`**: a stable per-database id on **every** response.
  Optional on reads, **required on writes** (missing → 428 Precondition Required;
  mismatch → 412 Precondition Failed). Its *presence on a read* is the clean
  probe signal that this Zotero has the write API at all.
- **`If-Unmodified-Since-Version`** guards updates and deletes against the
  library's `clientVersion` (mismatch -> 412 with expected/found). New batch
  creates have no prior object to guard. File uploads instead use
  `If-None-Match: *`. `?since=` and `If-Modified-Since-Version` gate reads;
  writes return `Last-Modified-Version` where applicable.

**Response shapes** (identical to Web API v3, so the parser is shared):
- Batch → 200 `{"successful":{"<i>":<obj>}, "success":{…}, "unchanged":{"<i>":"<key>"},
  "failed":{"<i>":{"key","code","message"}}}`.
- Single `PUT`/`PATCH`/`DELETE` -> 204. File registration -> 204.

**Managed attachment files** use the API-v3 full-upload contract for an existing
`imported_file` attachment. zotgo first creates attachment metadata without
`filename`, `path`, `md5`, or `mtime`, then:

1. Authenticated form POST to `/api/.../items/:key/file` with MD5, a bare
   filename, byte length, millisecond mtime, media type, and
   `If-None-Match: *`. `{"exists":1}` means Zotero already has those bytes.
2. When upload is needed, stream the staged bytes without API credentials to the
   returned exact same-origin `/api/local/uploads/:uploadKey` URL. The receiver
   verifies MD5 and returns 201. Redirects are not followed.
3. Authenticated form POST of `upload=:uploadKey` to the item file route; success
   is 204.

A focused item read then verifies the parent, `imported_file` link mode,
requested and actual filename, media type, MD5, and enclosure length. This is a
Local API workflow, not Connector ingestion: it targets an explicit existing
bibliographic parent. zotgo caps each import at 128 MiB while Zotero's Local API
receiver buffers the request before staging it.

**Versioning is endpoint-scoped, in Zotero's own words:** *"Local API versions
have no relation to Web API versions, nor … to local API versions returned by
other Zotero instances."* This is exactly zotgo's schema-2 decision to keep
`version` out of the DTOs — vindicated. `clientVersion` is a new per-library
counter, incremented once per library per transaction.

The already-shipped **Web API** provides official remote CRUD today (its writes
use the same batch response shape). The durable axis is local vs remote
endpoint, not read vs write.

## 2. Connector API (ingestion) — `/connector/*`

Served by `server_connector.js`. Same surface the browser extension uses; always
available when Zotero runs (no pref gate). Writes go through Zotero itself, so
`zotero.sqlite` integrity is Zotero's responsibility.

**Ingestion adapter only, not a general write backend.** `saveItems` writes to
`getSaveTarget()` — Zotero's currently-selected library/collection — and
silently redirects to My Library when that target is not editable
(verified in source). That nondeterminism disqualifies it for general
automation; use it only for app-mediated workflows (recognition, import,
snapshots). General resource writes belong on the official API write contract.

### Registered endpoints (from `server_connector.js`)

```
/connector/ping                      liveness ("Zotero is running")
/connector/detect                    run detection on a URL
/connector/getTranslators            list translators
/connector/getTranslatorCode
/connector/saveItems                 save translated item metadata → library
/connector/saveSnapshot              save a web snapshot
/connector/saveSingleFile            single-file snapshot
/connector/saveStandaloneAttachment  upload a local file as standalone attachment
/connector/saveAttachment
/connector/saveAttachmentFromResolver
/connector/getRecognizedItem         poll: did a saved PDF get recognized into a parent?
/connector/getSelectedCollection     current target collection in the UI
/connector/updateSession             attach tags/collection to a just-saved session
/connector/import                    import RIS / BibTeX / CSL text
/connector/installStyle
/connector/proxies  /connector/delaySync  /connector/getClientHostnames  /connector/hasAttachmentResolvers
```

### Write choreography (mined from pyzot's `write/`)

- **Local file** → `POST /connector/saveStandaloneAttachment` with raw bytes,
  `Content-Type`, and an `X-Metadata` header carrying `{sessionID, title, url:
  file://…}`. Response is `{canRecognize}` in current source; do **not** assume
  the new attachment key is returned.
- **Recognition** (PDF → parent metadata): poll `POST /connector/getRecognizedItem`
  with the sessionID — this is the *correct* API replacement for pyzot's SQL
  polling hack (`wait_for_recognized_parent` reaching into the DB). Current
  response shape is recognized parent display data (`title`, `itemType`), not a
  Zotero item key.
- **Tags / collection** → `POST /connector/updateSession` with the sessionID
  after the save.
- **Collection/library target** → `POST /connector/updateSession` expects a
  Zotero tree target (`L<libraryID>` or `C<collectionID>`). Resolve explicit
  `--collection` names through `POST /connector/getSelectedCollection`, whose
  `targets` array exposes those IDs for editable libraries/collections. Local
  API collection keys are not sufficient for `updateSession`.
- **RIS / BibTeX / CSL file** → `POST /connector/import` (no local parsing).
- **Identifier (DOI/arXiv/PMID/ISBN)** → resolve identifier to item JSON, then
  `POST /connector/saveItems`. The connector does *not* resolve identifiers for
  you headlessly; pyzot supplied its own resolvers.
- **Session model**: every write flow generates a client `sessionID` and threads
  it through save → recognize → updateSession.
- **Existing-item attachments/collection assignment**: connector attachment and
  session-update operations are session-bound. Attaching a file to an arbitrary
  pre-existing item, or assigning an arbitrary existing item to a collection,
  was handled in pyzot with direct SQLite writes. zotgo rejects that path; these
  capabilities are out of scope unless Zotero exposes an API for them.

---

## 3. Web API (remote reads) — `api.zotero.org` (verified live 2026-07-28)

The hosted Web API is API-v3 like the Local API, so the same semantic client
serves it — but several contract details differ and were **observed** against a
real key (user 8784047), not inferred:

- **No `/api` prefix; real user id.** Routes are `/users/<id>/…` and
  `/groups/<id>/…`. The `<id>` is the key owner's real numeric id (from
  `/keys/current`), not the Local API's `0` sentinel.
- **Auth.** `Zotero-API-Key: <key>` header (the documented current form; the
  `?key=` query param is deprecated and would leak the key into logs).
- **`GET /keys/current`** returns `{"userID", "username", "access":{"user":{…},
  "groups":{…}}}`. The `access` grants (`library`, `write`, `files`, `notes`)
  are the endpoint's own statement of what the key may do — so web capabilities
  are **probe-derived**, unlike the local write capability, which no probe can
  determine. A 403/404 here means the key is missing/revoked/wrong.
- **csljson is wrapped differently.** The Local API returns a bare array
  `[…]` per page; the **Web API returns `{"items":[…]}`**. zotgo unwraps both to
  one bare CSL-JSON array, so `zot export csljson` output is endpoint-neutral.
  *This bit us:* the httptest fakes served the local bare-array shape, so the
  merge passed every unit test and failed on the first real web export — the
  live web suite caught it (cf. the CSV-BOM finding).
- **Pagination differs for scoped exports.** An `itemKey=`-scoped `format=csljson`
  query returns all matches in **one** page (`limit` is ignored; `Total-Results`
  equals the match count, no `Link` next), where the Local API paginates the
  same query one item per page. The unwrap-and-splice merge handles both.
- **Rate limiting** (Web-only): `429`/`503` carry `Retry-After: <seconds>`
  (honored with bounded retries); a `Backoff: <seconds>` header asks the client
  to slow down (honored between paginated pages). The Local API never sends these.

## 4. What this buys zotgo over pyzot (concrete)

- No private-schema coupling (pyzot pinned SQLite schema v107).
- Reads while Zotero is **running** (pyzot's WAL lock effectively wanted it closed).
- `meta.*` server-derived fields for free (creatorSummary/parsedDate/numChildren).
- **Server-side export** (`format=bibtex|csljson`) → no `pybtex` reimplementation.
- Attachment file URLs via `/file` → no storage-dir path resolution.
- Recognition via `/connector/getRecognizedItem` → no DB polling hack.

## 5. What it costs

- Zotero must be running for **everything**.
- Local API is **off by default** → first-run enablement friction.
- Single logged-in user only.
- Identifier→metadata resolution is not provided by the local surfaces.

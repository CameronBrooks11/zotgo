# Design: write authority for agents

**Status:** Draft / RFC — under review. Not yet implemented.

This document defines how zotgo should authorize writes, so that an autonomous
agent can be given **bounded, time-limited, auditable** write access to a Zotero
library instead of an all-or-nothing switch. It supersedes the earlier working
rule of a hard "agents never write through this tool."

## Why this exists

zotgo's original safety posture was a blanket boundary: writes were treated as
off-limits for agents. That was a reasonable v1 stance, but it does not survive
contact with real use:

- Write features have landed (item/collection/tag writes, full-replace, and now
  managed-file upload). Each one silently widens what an agent *could* do if it
  is allowed to run zotgo at all.
- The useful workflow the maintainer actually wants is: *"let this agent work on
  this project for the next 30 minutes."* A blanket allow makes the blast radius
  the **entire library**; a blanket deny makes the agent useless for the task.

The goal is a middle path with a **contained blast radius**: a human grants a
narrow, expiring capability; the agent operates inside it; and afterwards the
human can see exactly what was — or could have been — touched, instead of asking
"do I need to restore my whole Zotero from backup?"

### Goals

1. A **human**, not an agent, decides when write access is granted.
2. Each grant is **scoped** (which libraries/collections, which operations).
3. Each grant is **time-boxed** (expires automatically).
4. Every write is **audited**, and the potential blast radius is knowable up
   front.
5. Writes **fail closed**: absent or expired authority means no write, with an
   actionable message (building on the fail-fast work in #42).

### Non-goals

- Multi-user / server authz. zotgo is a single-user local tool.
- Protecting against a fully compromised host. If an attacker already runs code
  as the user, no in-process boundary saves them; this raises the bar and
  contains *accidental* agent blast radius, it is not a sandbox.
- Replacing Zotero's own permissions where those are real (see below).

## Research: what Zotero can and cannot enforce

The decisive question is *where* the boundary can live. Zotero's own
authorization was investigated first, because a boundary the server enforces is
more robust than one the client promises.

| Capability the model needs | Zotero **Web** API | Zotero **Local** API (used by file upload) |
|---|---|---|
| Time-box / TTL on access | **No** — keys are valid indefinitely unless manually revoked | **No** — local key is single-use or persistent ("Always Allow") |
| Scope below a library (per collection/project) | **No** — per-library/group only | **No** — grants full local write |
| Mint a key programmatically | Only via a registered **OAuth** app + user handshake | Via the desktop **authorize modal** (a human approves) |
| Revoke a key programmatically | **Yes** — `DELETE /keys/<key>` | n/a |

Sources: Zotero Web API v3 docs — [basics](https://www.zotero.org/support/dev/web_api/v3/basics)
(key permissions, `DELETE` revocation, "valid indefinitely, unless revoked"),
[OAuth key exchange](https://www.zotero.org/support/dev/web_api/v3/oauth)
(programmatic key creation with requested permissions), and the local write
contract notes in [`docs/zotero-api.md`](../zotero-api.md).

**Conclusion:** Zotero cannot natively provide the two properties the model most
needs — an **expiring** grant and **sub-library scope** — and the Local API that
file upload depends on has neither scoping nor expiry at all. Zotero-side
authorization therefore **cannot be the primary mechanism**.

## Decision: enforce in the zotgo layer, harden with Zotero, harness as a belt

The write-authority boundary lives in **zotgo** as a *write lease* the tool
checks before every write. This is chosen over Zotero-side enforcement because,
per the research above, Zotero cannot express TTL or fine scope; and over the
harness layer as *primary* because a boundary tied to one agent runtime does not
protect the tool when driven another way.

The other layers still contribute, in their proper role:

- **Zotero-side (defense in depth):** for `--web` writes, require — and where an
  OAuth app is configured, mint and later `DELETE`-revoke — a **library-scoped
  write key**, so even a lease bug cannot write outside the granted library.
  This is real, server-enforced hardening; it just cannot stand alone.
- **Harness (optional belt):** an agent-config deny-rule on the grant command,
  so an agent literally cannot mint its own lease. Useful, but not relied upon —
  the tool must fail closed even if the harness is misconfigured.

```
        human ──mints──▶  write lease (scope + ops + expiry + audit)
                              │
   agent ──runs `zot …`──▶ zotgo ──checks lease──▶ Zotero write
                              │                        ▲
                              └─ no/expired/out-of-scope lease ⇒ refuse
   (--web only) lease scope ⊆ Zotero library-scoped key  ────────┘ (server-enforced)
```

## The write lease

A lease is a small local record (path under the existing config dir,
`~/.config/zotgo/`, `0600`; overridable with `ZOTGO_CONFIG_DIR`). Proposed shape:

```json
{
  "id": "lease_...",
  "created": "2026-08-20T15:00:00Z",
  "expires": "2026-08-20T15:30:00Z",
  "scope": {
    "libraries": ["user:0"],
    "collections": ["ABCD1234"],
    "operations": ["item.create", "item.patch", "attachment.import"]
  },
  "audit": "~/.config/zotgo/audit/lease_....jsonl",
  "note": "schmidma PBR project cleanup"
}
```

- **Minting — `zot grant` (human-only).** Prints exactly what it will authorize,
  then requires human confirmation. For the local endpoint the root of trust is
  Zotero's own **authorize modal** (an agent cannot click it); `zot grant` ties
  the lease to a successful authorize. A non-TTY / `--yes` invocation is refused
  for granting (the opposite of a normal command), so an agent cannot mint one
  non-interactively. The harness deny-rule is the additional belt.
- **Checking.** Every write command resolves the active lease and refuses unless
  it is present, unexpired, and the requested `(library, collection, operation)`
  is in scope. This composes with the existing `RequireWriteCapability` fail-fast
  (#42): capability first, then authority.
- **Audit &amp; blast radius.** Every write appends to the lease's audit log
  (operation, target keys, outcome). `--dry-run` already previews a write
  without performing it; combined with the lease scope, the human can see the
  *maximum* possible blast radius before granting, and the *actual* changes
  after.
- **Expiry &amp; revocation.** A lease past `expires` is dead — writes refuse and
  point at `zot grant`. `zot grant --revoke` ends it early. For `--web` with an
  OAuth-minted key, revocation also `DELETE`s the Zotero key.

## How existing and pending write commands conform

- `item create` / `patch` / `replace` / `delete`, `collection create` / `rename`
  / `move` / `delete`, `tag add` / `remove` / `delete` — each gains the lease
  check alongside its current `--yes` confirmation and capability probe. Their
  operation identifiers map to the `scope.operations` vocabulary.
- **#52 `attachment import`** — the pending managed-file upload. Its code is
  already reviewed clean (credential/redirect boundaries and staging TOCTOU
  defenses verified); it is held only until it can require an
  `attachment.import`-scoped lease. This is expected to be a small addition, not
  a rewrite.

## Open questions

1. **Lease integrity.** Does the lease need to be signed/HMAC'd, or is a
   `0600` file under the config dir sufficient given the single-user threat
   model? (Leaning: file perms are enough; signing adds little against a
   same-user attacker.)
2. **Collection scope semantics.** Zotero writes are not collection-scoped
   server-side. zotgo can enforce "only items in collection X" for `patch`/
   `delete` by pre-checking membership, but `create` places items *into* a
   collection. Define per-operation scope meaning precisely.
3. **Granularity of the operation vocabulary** — per-command (`item.patch`) vs
   per-class (`item.write`). Leaning per-command for tighter least-privilege.
4. **`--web` without an OAuth app.** If no OAuth app is registered, do we require
   the user to supply an already-library-scoped key and merely *verify* its
   grants (via the existing capability probe), rather than minting one?
5. **Interaction with "Always Allow".** The persisted local key already removes
   the modal on later writes; the lease becomes the thing that expires, so the
   modal is the mint-time gate, the lease is the runtime gate.

## Phased rollout (proposed)

1. **Lease core:** `zot grant` (mint/-\-revoke/-\-status), lease file, the
   `requireLease` check wired into all existing writes. Fail-closed + audit.
2. **#52 conforms:** add the `attachment.import` scope check; unblock and merge.
3. **`--web` hardening:** verify/mint library-scoped keys; optional OAuth
   mint+revoke.
4. **Docs + harness belt:** document the model in `writing.md`; add the agent
   deny-rule guidance.

Each phase is independently shippable and CI-gated, per the project's small-PR
convention.

# Design: write authority

**Status:** Implemented. Phases 1–2 have shipped and are live-verified against a
Zotero build with the local write API: `zot grant` leases, the deny-by-default
authorizer at the write chokepoint, and `attachment import` are in the tool
today, and phase 4's user documentation is in `writing.md`. Phase 3 (`--web`
writes) remains a roadmap item — see the issue tracker. This document is kept as
the design of record; the phased plan at the end records what has landed.

This document defines how zotgo authorizes writes, so that a non-interactive
caller — a script, a CI job, a cron sync, or an autonomous agent — can be given
**bounded, time-limited, auditable** write access to a Zotero library instead of
an all-or-nothing switch. It supersedes the earlier working rule of a hard
"agents never write through this tool."

The design is framed throughout around an autonomous **agent**, because
containing an agent that acts on your behalf without you watching is the sharpest
version of the problem and the motivating use case. The mechanism is general,
though: it keys off whether a human is present at the prompt, so every
non-interactive writer is bounded the same way — an agent is simply the example
that most needs it.

## Why this exists

zotgo's original safety posture was a blanket boundary: writes were treated as
off-limits for agents. That was a reasonable v1 stance, but it does not survive
contact with real use:

- Write features have landed (item/collection/tag writes, full-replace, and now
  managed-file upload). Each one silently widens what an agent *could* do if it
  is allowed to run zotgo at all.
- The useful workflow the maintainer actually wants is: *"let this agent work
  for the next 30 minutes."* A blanket allow makes the blast radius the **entire
  library**; a blanket deny makes the agent useless for the task.

The goal is a middle path with a **contained blast radius**: a human grants a
narrow, expiring capability; the agent operates inside it; and afterwards the
human can see exactly what was — or could have been — touched, instead of asking
"do I need to restore my whole Zotero from backup?"

## What this is (and what it is not)

Be precise about the threat model, because it determines what the lease can
honestly promise.

The lease is a **guardrail that contains a rule-following agent** — one that acts
only by invoking `zot` subcommands and honors the tool's refusals. For that
agent, the lease bounds scope, expires automatically, and records what happened.
That is the real, useful property: it contains **accidental** blast radius and
gives the human an audit trail and a knowable maximum reach.

The lease is **not a forgery-resistant authorization boundary** against a capable
or malicious same-user process. On a single-user host the agent *is* the user: it
can write the lease file directly, read any credential zotgo stores, or POST to
Zotero's local API without going through zotgo at all. This is **inherent to the
threat model, not a defect to be fixed with cryptography** — a signing key would
live in the same directory the attacker already controls (see [Q1](#q1-lease-integrity)).
The doc therefore never claims the lease "cannot be forged" or that "an agent
cannot mint its own lease"; it claims the lease contains an agent that plays by
the rules.

### Goals

1. A **human**, not an agent, mints a lease through an interactive step an agent
   cannot perform non-interactively.
2. Each grant is **scoped** — which library, which operations. (Sub-library
   *collection* scope is deferred; see [Q2](#q2-collection-scope-deferred).)
3. Each grant is **time-boxed** and expires automatically; there is no
   unexpiring lease.
4. Every write — and every refusal — is **audited**, and the potential blast
   radius is knowable before granting.
5. Writes **fail closed**: absent, expired, unreadable, or out-of-scope authority
   means no write, with an actionable, dimension-specific message (building on the
   fail-fast work in #42).

### Non-goals

- Multi-user / server authz. zotgo is a single-user local tool.
- Protecting against a fully compromised host or a malicious same-user process.
  If an attacker already runs code as the user, no in-process boundary saves
  them. This raises the bar against *accidents*; it is not a sandbox.
- Replacing Zotero's own permissions where those are real (see `--web` below).
- Tamper-resistant auditing. The audit log is a same-user-writable file: a
  convenience record for a well-behaved agent and forensics after an accident,
  not a trail that survives a hostile process.

## Research: what Zotero can and cannot enforce

The decisive question is *where* the boundary can live. Zotero's own
authorization was investigated first, because a boundary the server enforces is
more robust than one the client promises.

| Capability the model needs | Zotero **Web** API | Zotero **Local** API (used by file upload) |
|---|---|---|
| Time-box / TTL on access | **No** — keys are valid indefinitely unless manually revoked | **No** — local key is single-use or persistent ("Always Allow") |
| Scope below a library (per collection/project) | **No** — per-library/group only | **No** — grants full local write |
| Mint a key programmatically | Only via a registered **OAuth** app + user handshake | Via the desktop **authorize modal** (a human approves) |
| Revoke a key programmatically | **Yes** — `DELETE /keys/<key>` | n/a (no local revoke API) |

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
checks before every non-interactive write. This is chosen over Zotero-side
enforcement because, per the research above, Zotero cannot express TTL or fine
scope; and over the harness layer as *primary* because a boundary tied to one
agent runtime does not protect the tool when driven another way.

The other layers still contribute, in their proper role:

- **Zotero-side (defense in depth, `--web` only):** for `--web` writes, require a
  **library-scoped write key** and verify its grants are a subset of the lease's
  library scope (see [Q4](#q4-web-without-oauth)), so even a lease bug cannot
  write outside the granted library. This is real, server-enforced hardening; it
  applies only to `--web`, does not enforce TTL or sub-library scope, and does not
  apply to the local endpoint at all.
- **Harness (optional belt):** an agent-config deny-rule on `zot grant`, so an
  agent literally cannot mint its own lease. Useful, but not relied upon — the
  tool must fail closed even if the harness is misconfigured.

```
   human ──approves modal──▶ zot grant ──mints──▶ write lease
                                                  (scope + ops + expiry + audit
                                                   + bound write key)
                                                        │
  agent ──runs `zot … --yes`──▶ zotgo ──authorizer at writeRequest──▶ Zotero write
                                          │                              ▲
                                          └─ no/expired/out-of-scope ⇒ refuse (fail closed)
   (--web only) lease library scope ⊇ Zotero key grants  ──────────────┘ (server-enforced)
```

## The write lease

A lease is a small local record — a `0600` file under the existing config dir
(`~/.config/zotgo/`, itself `0700`; overridable with `ZOTGO_CONFIG_DIR`), written
with the same discipline as `cmd/zot/keystore.go`. There is **one active lease at
a time**. Shape:

```json
{
  "id": "lease_...",
  "created": "2026-08-20T15:00:00Z",
  "expires": "2026-08-20T15:30:00Z",
  "scope": {
    "libraries": ["user:0"],
    "operations": ["item.create", "item.patch", "attachment.import"]
  },
  "writeKey": "<local write key, bound to this lease>",
  "note": "PBR project cleanup"
}
```

- `scope.libraries` uses the **canonical** library token (`kind:id`) derived the
  same way at mint and enforcement; locally the user library is `user:0`, groups
  use their real id (see [Q6](#q6-canonical-library-identity)).
- `scope.operations` is a **closed, per-command vocabulary** (see [Q3](#q3-operation-vocabulary)).
- `writeKey` binds the credential *into* the lease (see [Bound key](#bound-write-key)),
  so expiry and revocation actually remove write ability.
- `scope.collections` is intentionally **absent in phase 1** (deferred, [Q2](#q2-collection-scope-deferred));
  the field is reserved so adding real collection enforcement later is non-breaking.
- The audit path is **derived from `id`** (`~/.config/zotgo/audit/<id>.jsonl`),
  not stored separately.

### Minting — `zot grant` (human-only)

`zot grant` is the only command that creates a lease, and it is deliberately the
**inverse** of every other write command:

- It **requires a TTY** and refuses `--yes` / non-interactive invocation. An
  agent cannot mint a lease non-interactively; the harness deny-rule is an
  additional belt. (The refusal message says *why* — minting needs a human to
  approve Zotero's authorize modal in an interactive terminal — and states
  explicitly that `--yes` cannot substitute here, so the reflex from other
  commands does not hit a bare wall.)
- The root of trust at mint time is **Zotero's own authorize modal** (local) — a
  desktop GUI a human must click; `zot grant` ties the lease to a successful
  authorize and stores the resulting key *in the lease*.
- It takes **`--ttl`** (e.g. `--ttl 30m`) with a bounded default (30m) and a
  documented maximum. There is no unexpiring lease.
- Before confirming, it **prints the concrete authorization** — resolved library,
  operations, and the count of items currently in scope — as the pre-grant
  blast-radius picture. (`--dry-run` remains the after-the-fact per-write preview;
  blast radius "before granting" cannot rely on it because it is a property of the
  write commands, not of `grant`.)
- Minting while a live lease exists **warns and replaces only on explicit
  confirmation**, so an agent's authority is never silently clobbered.

Surface: `zot grant`, `zot grant status`, `zot grant revoke` (noun-verb
subcommands matching `item create` / `collection rename`; no `--revoke`/`--status`
flag-verbs).

### Checking — a deny-by-default authorizer at the write chokepoint

Enforcement lives in **`internal/zotero` at the single `writeRequest`
chokepoint**, not sprinkled across the twelve `cmd/` write actions. The `Client`
holds a `WriteAuthorizer` interface; a **nil authorizer denies**. `cmd/zot`
injects an implementation that reads the lease file (keeping file/CLI logic out
of the dependency-light client). Every write funnels through `writeRequest`, so a
new or forgotten write path (the reason #52 was held before it merged) is
structurally unable to skip the check.

The write methods thread a `(library, operation)` scope descriptor to
`writeRequest`. Ordering composes with the existing fail-fast layering:
`RequireWriteCapability` (capability) first, then the lease (authority).

**Interactive human writes do not require a lease.** A human at a TTY who answers
the existing `confirm()` prompt is their own authority. The lease is required only
for **non-interactive / `--yes` / machine-mode** writes — the case where a human
is *not* present. This maps the lease precisely to its purpose and avoids a
friction regression (and lockout footgun) on the maintainer's own manual edits.

### <a id="bound-write-key"></a>Bound write key — writes never self-authorize

When leases are in force, zotgo does **not** persist a standalone "Always Allow"
key. The local write key lives **inside the lease record**, and write commands
**never trigger `Client.Authorize`** — only `zot grant` does. Consequences:

- A forged or hand-written lease carries no valid key, so the write `401`s /
  refuses even though the file exists.
- Expiry and `zot grant revoke` actually remove write ability, instead of leaving
  a broader, longer-lived Zotero credential behind that a re-forged lease could
  replay.

This is the single change that gives the authority layer real teeth against an
*accidental* same-user agent, and it closes the "Always Allow" replay hole
([Q5](#q5-always-allow)). Residual honesty: a process can still POST to Zotero's
local API directly, and Zotero has no local API to forget an "Always Allow" key,
so `zot grant revoke` removes zotgo-side state only — documented as a limitation
in `writing.md`.

### Fail-closed, with dimension-specific messages

Any lease that is missing, unreadable, malformed, expired, or out-of-scope
**denies** the write (a parse error never defaults to allow). Each refusal
dimension gets a distinct sentinel and message, wired through `writeFriendly`
alongside the existing `ErrWrite*` sentinels:

| Condition | Message shape |
|---|---|
| No lease | `no active write lease; run 'zot grant' to authorize writes` |
| Expired | `write lease expired at <ts>; run 'zot grant' to renew` |
| Wrong operation | `lease does not permit item.delete; re-grant with that operation` |
| Wrong library | names the target library vs the granted one |

Dimension-specific errors are what let an agent report *which* boundary it hit so
a human can re-scope.

### Audit — every decision, allowed and refused

Every authorization decision — allowed **and** refused/out-of-scope/expired —
appends a record (timestamp, operation, library, decision, and the refusal reason)
to the lease's audit log. Recording refusals matters because a burst of them is
exactly the signal a worried maintainer wants. `zot grant status` surfaces it: the
active lease, its expiry, the audit path, and a decision summary (N allowed, M
refused). The JSONL file is directly inspectable.

Two honest limitations of the phase-1 audit, both planned enrichments rather than
gaps to hide:

- Records name the *operation and library*, not the individual *target keys* — the
  authorizer sees the operation and library, not the request body. Adding per-object
  target keys means threading them to the authorizer and is deferred.
- A record is written when a write is *authorized*, before the HTTP write; an
  allowed write can still fail Zotero's own preconditions afterwards, so the summary
  counts authorization decisions, not confirmed writes. A formatted `zot grant log`
  and per-write outcomes are a later refinement.

The audit file is same-user-writable, so it is a convenience/forensics record, not
a tamper-resistant trail — stated plainly so it is not oversold.

## How the write commands conform

- `item create` / `patch` / `replace` / `delete`, `collection create` / `rename`
  / `move` / `delete`, `tag add` / `remove` / `delete` — each gets the centralized
  authorizer check (nothing per-command to add beyond threading the operation id).
  Their operation identifiers are the closed vocabulary in [Q3](#q3-operation-vocabulary).
- **`attachment import`** (#52) — the managed-file upload. It conforms the same
  way, adding the `attachment.import` operation identifier to the vocabulary
  rather than a bespoke check; its credential/redirect boundaries and staging
  TOCTOU defenses were verified in review before it merged.

## Resolved questions

### <a id="q1-lease-integrity"></a>Q1 — Lease integrity: **no signing**

Do **not** sign/HMAC the lease. A `0600` file under the `0700` config dir is the
whole mechanism; trust is anchored in Zotero's authorize modal at mint time. A
same-user agent can read any signing key that lives beside the lease, so a
signature adds zero forgery-resistance under the stated threat model. Permissions
are set on **write** (as `keystore.go` already does); on **read**, the tool
parses against the schema, requires a valid unexpired `expires`, verifies scope,
and fails closed on anything malformed. It does **not** add strict read-side gates
(owner check, exactly-`0600`, parent-dir mode) — those harden a declared non-goal,
are inconsistent with the unguarded key sitting in the same dir, and risk false
lockouts on benign umask/backup/network-FS states.

### <a id="q2-collection-scope-deferred"></a>Q2 — Collection scope: **deferred to library-level**

Phase 1 enforces **library-level scope only**. Zotero enforces nothing below a
library server-side, so collection scope would be client-only (TOCTOU-adjacent,
no backstop) with genuinely ambiguous create-into-collection and subtree
semantics, and `tag.delete` cannot be bounded below a library at all. For a
single user — typically one user library — library scope + a 30-minute TTL +
audit already delivers a contained blast radius; collection scope is the largest
bug surface for the least marginal safety. `scope.collections` is reserved in the
JSON as an accepted-but-unenforced field so real enforcement is a non-breaking
add later.

**If collection scope is added later**, the membership rule **must** be the
two-sided freeze invariant `pre\scope == post\scope` for any membership-changing
write — **not** `post ⊆ pre ∪ scope`, which still permits silently stripping an
item out of an out-of-scope collection (the `write.go` clearing path — data loss).
Parented creates and `attachment.import` must resolve the parent item's
collections (a read) to determine destination scope, because child items inherit
membership and carry an empty `data.collections`. Recorded here so it is not
re-litigated.

### <a id="q3-operation-vocabulary"></a>Q3 — Operation vocabulary: **per-command, closed, fail-closed on unmapped**

`scope.operations` is a closed, per-command vocabulary that exactly matches the
write command surface and reuses the `Operation` labels the code already emits:

```
item.create  item.patch  item.replace  item.delete
collection.create  collection.rename  collection.move  collection.delete
tag.add  tag.remove  tag.delete
attachment.import
```

Any write whose operation id is absent from `scope.operations` **refuses**
(default-deny, including future unwired commands). Per-command is vindicated by
`item.replace` (destructive full overwrite, must be separable from `patch`) and
`tag.delete` (library-wide, must be separable from per-item `tag.remove`) —
distinctions a per-class `item.write` would collapse. The common "clean up
metadata" grant is therefore verbose; wildcards/presets are deferred as purely
additive sugar.

### <a id="q4-web-without-oauth"></a>Q4 — `--web` without OAuth: **verify-only**

For `--web`, require the user to supply an already-library-scoped Web API key and
merely **verify** its grants; do **not** build OAuth mint/revoke (OAuth 1.0a is
hundreds of fiddly, dependency-tempting lines for a defense-in-depth layer on a
path that is not the driving use case — YAGNI). Verify-only cannot manufacture
authority and matches Zotero's real per-library capability; TTL still comes from
the lease. Enforce `key_write_libraries ⊆ lease.scope.libraries` (subset, not
equality); walk `Access.User` and `Access.Groups` (including the special `all`
entry) rather than reusing the boolean `grantsWrite()`; normalize `user:0` vs
`user:<realid>` and **fail closed on any unmappable library identity**. zotgo
never `DELETE`s a user-supplied key it did not mint.

Note: the CLI has **no `--web` write path today** (writes are local-only). Phase 3
is therefore "build `--web` writes, then verify," and `RequireWriteCapability`
currently returns nil for the web endpoint — real, modest work, scoped as its own
phase.

### <a id="q5-always-allow"></a>Q5 — "Always Allow": **modal is the mint gate, lease is the runtime gate**

Zotero's authorize modal is the **mint-time** gate (enforced by `zot grant`'s
TTY-required / `--yes`-refused confirmation); the lease is the **runtime** gate
(expiry + scope). Binding the key into the lease and never re-authorizing on the
write path (above) means a persisted "Always Allow" grant cannot be replayed by a
forged lease. The premise that "Always Allow" suppresses the modal on later
authorizes is **provisional** — `docs/zotero-api.md` documents single-use only for
plain "Allow" — and is marked pending live verification; the conclusion holds
either way, since a silent re-authorize would equally defeat a per-write modal.

### <a id="q6-canonical-library-identity"></a>Q6 — Canonical library identity

Pin **one** canonical library token derived the same way at mint and at
enforcement, so the two always agree. In phase 1 that token is
`LibraryRef.Kind:LibraryRef.ID` — and locally the user library's id is the `0`
sentinel everywhere (routing, `selfUser`, and the lease alike), so both sides read
`user:0` and match; groups use their real id. The token is never *resolved* from a
Zotero response, which is where the mismatch risk lives: the Local API accepts `0`
on input but reports the real id in envelopes, so keying a lease off a response id
while routing on `0` would be a silent fail-open/lockout — the #1 correctness risk
in `docs/zotero-api.md`. Real-numeric-id canonicalization only becomes relevant for
`--web` (phase 3), where the user id is real on both sides. A table test covers the
`user:0` and group forms.

## Phased rollout

1. **Lease core.** *(Shipped.)* The `0600` single-lease file (id, created, expires, library
   scope, per-command operations, bound write key, note); `zot grant` /
   `grant status` / `grant revoke`, TTY-gated and tied to a successful Zotero
   authorize, with `--ttl` (30m default, documented max) and a printed pre-grant
   blast radius; a deny-by-default `WriteAuthorizer` injected into the `Client`
   and enforced at `writeRequest`, applied **only** to non-interactive/`--yes`/
   machine writes; per-command operation scope with fail-closed on any unmapped
   op; a canonical library token (`user:0` locally — see Q6); dimension-specific
   refusal sentinels; and an append-only JSONL audit of every decision (allowed and
   refused), surfaced by `grant status`.
2. **#52 conforms.** *(Shipped.)* Add `attachment.import` to the vocabulary;
   unblock and merge.
3. **`--web` hardening.** *(Roadmap — see the issue tracker.)* Build the `--web`
   write path, then verify a user-supplied library-scoped key (subset check,
   fail-closed on unmappable identity). OAuth mint/revoke is **not** in scope —
   deferred indefinitely until a concrete demand justifies the code.
4. **Docs + harness belt.** *(User docs shipped in `writing.md`.)* Document the
   model (including the honest limitations and the local-key residual); add the
   caller deny-rule guidance.

Each phase is independently shippable and CI-gated, per the project's small-PR
convention. The lease logic is unit-tested against fakes, and per the project's
iron rule the shipped phases were confirmed by a live run against a Zotero build
with the write API (zotero/zotero#5015, released in Zotero 10.0): the
authorize/key/precondition interplay the lease wraps is verified live.

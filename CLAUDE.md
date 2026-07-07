# CLAUDE.md

The canonical working agreement for this repository is [`AGENTS.md`](AGENTS.md).
Read it first; it is the source of truth for build, test, and contribution
conventions.

Key reminders:

- zotgo talks to a **running Zotero 7+** over HTTP (Local API for reads,
  Connector API for writes). **Never** open `zotero.sqlite`.
- Run `just check` and `just test` before every commit; both must be green.
- Conventional Commits; keep the SDK (`internal/zotero`) dependency-light and
  free of CLI concerns.

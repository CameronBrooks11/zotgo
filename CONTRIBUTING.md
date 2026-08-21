# Contributing to zotgo

Thanks for contributing. This file is the quick entry point; the full working
agreement — architecture, conventions, and the reasoning behind them — lives in
[`AGENTS.md`](AGENTS.md) and is the source of truth for humans and AI agents
alike. Read it before a non-trivial change.

## Before you start

- zotgo talks to a **running Zotero 7+** over HTTP. There is no database access:
  never open `zotero.sqlite`. If a capability is not exposed over the API, zotgo
  does without it.
- The runtime dependency set is the standard library plus one CLI module. Adding
  a dependency needs a clear, justified reason.

## The local gate

Every change must pass the same gate CI runs. Install it once, then it runs on
every commit:

```sh
just setup   # installs deps and the pre-commit hook
just check   # gofmt + vet + staticcheck + misspell + compile
just test    # the full suite
```

`just setup` installs a pre-commit hook that runs `just check && just test`, so a
normal `git commit` enforces the gate. **Do not bypass it** with `--no-verify`.
Tests must be platform-independent — CI runs Linux, macOS, and Windows (see the
testing notes in [`AGENTS.md`](AGENTS.md#conventions)).

## Commits and pull requests

- **Conventional Commits**: `type(scope): description`, imperative mood,
  lowercase, no trailing period. One logical change per commit. The full list of
  types is in [`AGENTS.md`](AGENTS.md#conventions).
- Keep PRs focused; base them on current `main`.
- The PR template asks for a summary, rationale, and the testing you ran. Fill it
  in — "what I changed and how I know it works" is what gets a PR merged quickly.
- **Machine-readable output is a contract.** The DTOs in `internal/output` are
  versioned; renaming, re-meaning, or removing a field is a breaking change. See
  the contract rules in [`AGENTS.md`](AGENTS.md#conventions) before touching
  `--json`/`--jsonl` shapes.

## AI-assistance disclosure

zotgo is developed with AI assistance, and contributions made with it are
welcome. If you used an AI tool (Claude, Copilot, etc.) in a meaningful way,
please say so in the PR — a single line is enough. This is about transparency,
not judgment: you are responsible for understanding and standing behind every
line you submit, however it was written.

## Reporting bugs and requesting features

Open an issue using the templates. Bug reports should include the environment
block (zotgo version · Zotero version · endpoint · OS) so a report is
reproducible. Security issues follow [`SECURITY.md`](SECURITY.md) instead —
please do not open a public issue for them.

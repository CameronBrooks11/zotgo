# Security Policy

## Reporting a vulnerability

Please report security issues **privately**, not as a public issue or pull
request.

Use GitHub's private vulnerability reporting: on the repository's **Security**
tab, choose **Report a vulnerability**. This opens a private advisory visible
only to the maintainers.

Include enough to reproduce: what you did, what happened, and the environment
(zotgo version · Zotero version · endpoint · OS). We will acknowledge the report
and keep you updated as we investigate.

## Supported versions

zotgo is pre-1.0 and moves fast; fixes land on `main` and in the next release.
Please report against the latest release or `main`.

## Scope and threat model

zotgo is a local command-line client for a Zotero desktop app you run yourself.
A few properties are worth stating so reports land on real issues:

- **No database access.** zotgo never opens `zotero.sqlite`; all access is through
  Zotero's HTTP API. Reports that assume direct database access are out of scope.
- **Local trust boundary.** The Local API is unauthenticated and single-user by
  design; the `zot grant` write lease is an accident guardrail for a rule-following
  caller, not a defense against a hostile process already running as your user.
  See [`docs/design/write-authority.md`](docs/design/write-authority.md).
- **No secrets in the repo or logs.** Never commit tokens, API keys, or
  credentials. If you find any committed secret or a path that logs one, report
  it privately.

Reports about credential handling, path traversal in file operations, unsafe
handling of API responses, or anything that lets one user's zotgo harm another's
data or machine are in scope and appreciated.

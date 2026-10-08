# Secret-scan findings

A full-history secret scan, triaged. Read this before treating a `gitleaks`
result as a live leak, and before proposing a secret-scanning gate for CI.

| | |
|---|---|
| Command | `gitleaks git . --redact` |
| Scanner | gitleaks 8.30.1 (mise) |
| History scanned | 655 commits, ~8.0 MB |
| Findings | **15**, across 8 files and 8 commits |
| Earliest / latest | 2026-03-23 / 2026-09-19 |
| Findings that are live secrets of ours | **0** |

## This is not a CI gate

No workflow runs `gitleaks` or `trufflehog`; `.github/workflows/` has no secret
scan at all. The only reference in the repo is a documented habit in
`docs/superpowers/plans/2026-10-07-artist-provenance-and-repoint.md`:

```bash
gitleaks git --staged --redact
```

That scans the **index**, so it never sees history. Every finding below is
therefore from a commit that a staged scan could not have covered, which is the
whole reason they sat unnoticed rather than the reason they are dangerous.

## The findings

`line` is the current line where the file still exists; for a finding introduced
in a revision that has since moved on, it is the line at that commit.

| # | Rule | File | Line | Introduced by | Date | Verdict |
|---|---|---|---|---|---|---|
| 1 | `curl-auth-user` | `ops/docs/library-dedup-runbook.md` | 138 | `b190c70` | 2026-09-11 | Placeholder |
| 2 | `curl-auth-user` | `ops/docs/library-dedup-runbook.md` | 205 | `2497f01` | 2026-09-18 | Placeholder |
| 3 | `curl-auth-user` | `ops/docs/library-dedup-runbook.md` | 208 | `2497f01` | 2026-09-18 | Placeholder |
| 4 | `generic-api-key` | `.env.e2e.example` | 23 | `5d29d29` | 2026-09-15 | Test fixture |
| 5 | `generic-api-key` | `ops/fake-slskd/entrypoint.py` | 43 | `362b386` | 2026-09-19 | Test fixture |
| 6 | `generic-api-key` | `backend/internal/services/spotify_spdc.go` | 88 | `d70a974` | 2026-05-25 | **Third-party, public** |
| 7 | `generic-api-key` | `.slim/cartography.json` | 31 | `4f957b1` | 2026-03-23 | False positive |
| 8 | `generic-api-key` | `.slim/cartography.json` | 38 | `4f957b1` | 2026-03-23 | False positive |
| 9 | `generic-api-key` | `.slim/codemap.json` | 41 | `8b25212` | 2026-05-24 | False positive |
| 10 | `generic-api-key` | `.slim/codemap.json` | 42 | `8b25212` | 2026-05-24 | False positive |
| 11 | `generic-api-key` | `.slim/codemap.json` | 169 | `8b25212` | 2026-05-24 | False positive |
| 12 | `generic-api-key` | `KimiAgentRefactor_djinn-netrunner/netrunner/.slim/codemap.json` | 41 | `ebac6b4` | 2026-05-29 | False positive |
| 13 | `generic-api-key` | `KimiAgentRefactor_djinn-netrunner/netrunner/.slim/codemap.json` | 42 | `ebac6b4` | 2026-05-29 | False positive |
| 14 | `generic-api-key` | `KimiAgentRefactor_djinn-netrunner/netrunner/.slim/codemap.json` | 169 | `ebac6b4` | 2026-05-29 | False positive |
| 15 | `generic-api-key` | `KimiAgentRefactor_djinn-netrunner/netrunner/.slim/cartography.json` | 38 | `ebac6b4` | 2026-05-29 | False positive |

## Why each class is not a leak

### Placeholders in a runbook — 3 findings

`ops/docs/library-dedup-runbook.md` contains a copy-pasteable Navidrome
re-index command:

```bash
curl -s -u "admin:PASS" "http://navidrome:4533/rest/startScan.view?u=admin&p=PASS&v=1.16.1&c=netrunner&f=json"
```

`PASS` is the literal placeholder. The `curl-auth-user` rule matches the shape
`-u user:pass` and cannot tell a placeholder from a real password, so it fires
on the documented form exactly as it would on a live credential. Nothing to
rotate; the runbook is meant to be filled in by the operator.

### The checked-in e2e test key — 2 findings

`.env.e2e.example` and `ops/fake-slskd/entrypoint.py` carry the same
`e2e-`-prefixed key. It is deliberate: the fake slskd and the throwaway e2e
stack must agree on it, nothing outside the e2e compose network consumes it, and
the example file is what a developer copies from. The `e2e-` prefix is the tell.

### spotDL's published OAuth client — 1 finding

`backend/internal/services/spotify_spdc.go`:

```go
// SpotDL's well-known Spotify OAuth client credentials.
// Used for client_credentials flow — public data only, no 429 blocks.
spotdlClientID     = "5f573c9620494bae87890c0f08a60293"
spotdlClientSecret = "212476d9b0f3472eaa762d90b19b0ba8"
```

This is a **real credential, and the only finding that is one** — but it is
spotDL's, published in that project's own source and shared by every spotDL
user, not a NetRunner secret. It cannot be rotated by us, so the scanner will
report it forever. The forward-looking risk is not ours to leak: it is that if
Spotify ever revokes it upstream, this service breaks and there is no owner for
the rotation. Worth knowing; not worth an incident.

### Path-to-hash maps in agent index files — 9 findings

Every one of the nine is the same shape: a generated index that maps a source
path to a 32-hex-character content hash.

```json
"backend/internal/api/auth.go": "fc7beb7b…",
"backend/internal/api/auth_context.go": "c17523a1…",
```

The `generic-api-key` rule keys on a keyword list that includes `auth`, which
here matches a **filename**, and the hash beside it has enough entropy to look
like a key. The matched values are content hashes.

Five of the nine are the current, **tracked** files (see below); four are in
`KimiAgentRefactor_djinn-netrunner/`, a path that no longer exists, so they are
history-only.

## `tracked` matters: the index files are committed

`.gitignore` line 29 covers only `.slim/clonedeps/`, so these are tracked and
rewritten in place:

```
$ git ls-files .slim/
.slim/cartography.json
.slim/clonedeps.json
.slim/codemap.json
```

They are generated agent-tooling artifacts — a dependency clone index and two
code/architecture maps — that happen to live in the checkout. That is a separate
question from the scan (do they belong in the repo?), but it is why five of the
nine false positives can reappear at any time: regenerate the map and the same
paths land beside new hashes.

## What a working-tree scan adds, and why it is not the same number

`gitleaks dir .` reports **59**, which mostly measures local state rather than
the repository:

| Where | # | What |
|---|---|---|
| `.art/gl/gl.json` | 13 | A previous report quoting itself |
| `graphify-out/cache/stat-index.json` | 12 | Local graphify cache, untracked |
| `.env` | 9 | The developer's real local env file — correctly gitignored (`.gitignore:2`) and untracked |
| `.orca/worktrees/…` | 12 | A second checkout of this repo inside an Orca worktree, scanning the same shapes |
| `.env.e2e` | 1 | Local e2e env, gitignored |
| Tracked repo files | 12 | `.slim/codemap.json` 3, the runbook 3, `.slim/cartography.json` 1, `.env.e2e.example` 1, `.env.example` 1, `.env.release.example` 1, `spotify_spdc.go` 1, `entrypoint.py` 1 |

So the two numbers answer different questions. **History is the committed
surface**; `dir` includes gitignored local files and other checkouts. Quote the
history number when talking about what the repository leaks.

## Decisions

1. **No rotations.** Nothing in the 15 is a NetRunner credential.
2. **No CI gate yet.** A gate added today would fail on 15 findings that are all
   benign, and the pragmatic response to a red gate is to allowlist broadly —
   which teaches the team to ignore it. Fix the signal first.
3. **If a gate is wanted,** the prerequisite is a `.gitleaks.toml` allowlist for
   the two shapes that produce the 14 false positives and fixtures: the
   path-to-hash map (`generic-api-key` beside a 32-hex value in
   `.slim/*.json`) and the `e2e-` test key. With those allowlisted, a full scan
   should report exactly the one spotDL entry, and that one deserves a comment
   in the config saying why it is accepted.
4. **Separate question, unanswered:** whether `.slim/codemap.json`,
   `.slim/cartography.json` and `.slim/clonedeps.json` belong in the repo at
   all. They are agent artifacts, they churn, and they are the source of nine of
   the fifteen findings.

## Reproducing

```bash
gitleaks git . --redact                      # history: 15
gitleaks dir . --redact                      # working tree: 59 (see above)
gitleaks git . --redact --report-format json --report-path /tmp/gl.json
```

`--redact` keeps matched secret bytes out of the report; use it whenever the
report will be stored, pasted, or committed.

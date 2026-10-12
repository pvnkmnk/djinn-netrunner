# UI visual harness

Batched before/after capture of the real NetRunner console, for the UI pass
(Linear I-4 / P-DJI-52 / P-DJI-53 / P-DJI-54) to verify visually.

It is **not a test suite**: it makes no assertions about the app. One command
records what the running stack actually rendered into a named batch; a second
pair of batches is diffed into a report a reviewer can read. Nothing here
touches `ops/web/` — the polish is the queued work, this is its measurement.

## Use

```bash
# a running stack is required (docs/DEPLOYMENT.md); point at it if not on :8080
export UI_VIS_BASE_URL=http://127.0.0.1:8080

scripts/ui-visual.sh mint-session      # once: a session for the seeded-content account
scripts/ui-visual.sh capture before    # every named surface -> e2e/visual-output/before/
# ... make the UI change ...
scripts/ui-visual.sh capture after
scripts/ui-visual.sh compare before after
```

`compare` writes `e2e/visual-output/compare-before-after/` containing
`compare.html` (before | after | diff heatmap for every surface, changed
surfaces first), `compare.json` (the same numbers, machine-readable) and
`diff/*.png` heatmaps. Open `compare.html` in a browser.

`capture` exits non-zero if any surface failed to render — a surface that did
not render is a failed capture, not a zero-diff one.

## Surfaces

`surfaces.json` is the surface list, and it is data. Every entry is either a
real route from `backend/cmd/server/main.go` (`setupRoutes`), an interaction
that opens one of the `.impeccable/surfaces/` briefs, or a fragment the pass
will touch. `scripts/ui-visual.sh list` prints them.

Three kinds:

| kind | meaning |
| --- | --- |
| `page` | navigate a route and shoot it; `waitFor` holds until an htmx region has swapped in |
| `interaction` | navigate `mount`, run `steps`, then shoot — this is how the two surface briefs are reached, because both render into `#modal-container` |
| `partial` | a fragment served alone, with `/static/css/styles.css` injected so it is not shot as bare HTML |

Step verbs: `goto`, `click` (with `nth`), `waitFor`/`waitForSelector`,
`waitForContent` (element has non-whitespace text), `waitForLoadState`.

Each surface is captured at two viewports (`desktop` 1280x800, `mobile`
390x844); both are shot full-page, with animations disabled, caret hidden, and
`prefers-reduced-motion`/dark/UTC pinned so a pair differs only where the UI
differs.

## What a diff means

Every row also carries `dom: same|changed` -- a hash of the markup the shot
shows, with the masked regions removed. It separates *the markup moved* from
*the pixels moved*: markup and pixels both changing is the change under
review, pixels changing while the markup holds still is a rendering or
timing difference, and markup changing with no UI edit is the **data behind
the surface** (the seeded library is not frozen -- a scan adding two tracks
shifts every browse row below them). Read a diff with that column before
blaming the change.

`#console-logs` and `#console-socket` are masked by default (`defaultMask` in
`surfaces.json`) — the live console streams over a websocket and would differ
on every capture for no design reason, so it is painted over rather than
reported. Anything else that moves on its own (stat counters, job durations)
is left visible on purpose: it is real content, and the report tells you which
surface moved and by how much, so a 2% dashboard diff reads as live counters
while a 40% track-detail diff reads as the change you made. Mask a selector
per surface only if it is genuinely noisy and not under review.

## Auth

First match wins:

1. `--session <id>` / `UI_VIS_SESSION` — a `session_id` cookie value
2. `e2e/visual-output/.session` — written by `mint-session` (gitignored)
3. `UI_VIS_EMAIL` + `UI_VIS_PASSWORD` — log in as that account
4. register + log in the default harness account (an empty library)

Mode 1/2 is what makes captures show seeded content: the seeded library belongs
to `task6.beta@smoke.test`, whose dev password is not recorded anywhere, so
`mint-session` inserts one expiring `sessions` row instead of resetting it.
The row is a bearer credential — `capture` writes it only to the gitignored
output root, and it expires in 12 hours.

`/admin` is `AdminOnly`; the seeded account is not an admin, so that surface
captures the refusal. Point `UI_VIS_EMAIL` at an admin account to capture the
real page.

## Requirements

- Playwright + Chromium, already vendored in `e2e/` (`node e2e/visual/*.mjs`
  resolves them from `e2e/node_modules`; no install step).
- Docker only for `mint-session` (it needs the stack's postgres).
- Git Bash on Windows is fine — the wrapper is LF-only bash.

## Output layout

```
e2e/visual-output/            (gitignored)
├── .session                  session_id for seeded-content capture
├── before/  after/           <surface>.<viewport>.png + manifest.json
└── compare-before-after/     compare.html, compare.json, diff/*.png
```

# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

**Primary: the single operator.** One person who runs the appliance on their own
hardware and wants their music library to fill itself — watch a source, acquire
what it finds, verify it is the right recording, drop it into a library a media
player can read. The UI is a server-rendered operations panel, not a media
browser; Navidrome or any Subsonic client is where they actually listen. This is
the default path and the one the product is designed around.

**Secondary: a small household or team sharing one instance.** Confirmed real,
not hypothetical. Each user has their own libraries, watchlists and job history,
isolated per owner. Multi-user scoping exists so a second account never clobbers
the first — it is a correctness property of the single-operator case as much as a
feature for the multi-operator one.

The distinction matters in one direction more than the other: designs should
optimise for one operator's speed and legibility, and must never leak another
owner's data when a second account exists.

## Product Purpose

NetRunner turns a server into a music acquisition appliance. It watches sources
(artist monitoring, Spotify/Last.fm/ListenBrainz/Discogs/RSS watchlists, local
directories), searches for what is new, acquires it over Soulseek, verifies it is
the right recording, enriches it with MusicBrainz and AcoustID metadata, and
routes it into a configured music library.

Success is not "the UI looks good". It is: **a release the operator wanted shows
up in their library, correctly tagged, without them babysitting it.** Everything
in the product either moves that outcome forward or gets out of its way.

## Positioning

The mechanism a neighbouring product could not truthfully copy: **the
acquisition decision is made by the pipeline, and the operator's job is to judge
it, not to drive it.** Preview before acquiring, quality profiles that hold a
source to a standard, provenance recorded per track, and a Shadow Cache so a
metadata outage degrades rather than destroys.

The appliance is self-hosted and privacy-first, and the Subsonic endpoint
authenticates per request rather than through the browser session.

Egress confinement is real but **path-dependent, and the difference matters**:
`docker-compose.release.yml` and `docker-compose.e2e.yml` run a `squid` sidecar
(`ubuntu/squid`) whose `allowed-domains.txt` decides what may be fetched, while
plain `docker-compose.yml` — the dev path — runs **no proxy service at all** and
only passes `PROXY_URL` through. So the boundary is enforced in production and
in CI, and absent while developing. Do not describe it as unconditional.

## Operating Context

- **Deploy paths are two and both are documented.** `./scripts/deploy.sh` for
  dev, `./scripts/deploy.sh --release` for a tagged release build. Both are
  exercised end to end, including version stamping into both images and the
  version footer. A change that only works on one path is not done.
- **Operations are console-first.** `docs/RUNBOOK.md` is the day-two path;
  `docs/DEPLOYMENT.md` is install. A queued job's only diagnostic path should be
  the UI — position, reason, and an in-product remedy — not a shell, `psql`, and
  a worker log.
- **Verification is layered.** `scripts/smoke.sh`, `scripts/e2e.sh`,
  `scripts/integration-tests.sh`, `scripts/mutation-check.sh`, plus `review` /
  `quality` / `test` / `integration` CI jobs. `docs/BETA_ACCEPTANCE.md` is the
  evidence record; `docs/BETA_ACCEPTANCE_EVIDENCE.md` holds the measurements.
- **The product is judged by driving the real UI.** Every defect in the current
  playtest project was found by using the app as a first-time user on a live
  stack, not by reading the code. That is the standing method.
- **Soulseek requires an operator account.** `slskd` carries it. Nothing in the
  pipeline works without it, so it is a documented prerequisite, not a setting.

## Capabilities and Constraints

Confirmed functionality: watchlists (Spotify liked/playlists/discover, Last.fm,
ListenBrainz, Discogs, Lidarr, RSS, local files); artist monitoring via
MusicBrainz; scheduled sync; parallel acquisition over Soulseek; MD5
deduplication; MusicBrainz + AcoustID enrichment; per-source quality profiles;
library browse; job queue with fair claiming, cancel and retry; a dashboard
with a live console; an admin surface for users, audit log and settings; an MCP
server (`backend/cmd/agent`, on `mark3labs/mcp-go`) and a CLI
(`backend/cmd/cli`) for autonomous management.

Terminology as the product uses it: *watchlist* (a source of candidates),
*artist* (a monitored MusicBrainz entity), *library* (a configured destination
path), *job* (one unit of pipeline work), *quality profile* (the bar a source is
held to), *shadow cache* (the metadata cache).

Constraints that bind future work:

- **Owner scoping is load-bearing and must stay.** `libraries.path` carries a
  global unique index while every list query is owner-scoped. That asymmetry is
  real and has already caused one urgent defect; the resolution was adoption of
  owner-less rows, not dropping owner scoping.
- **A convention the codebase believes is inert.** `hx-disabled-elt` does nothing
  here, and htmx's `htmx-request` class is not reliably cleared. Nothing may
  assert a pending-state convention without measuring it live first.
- **htmx does not swap a 4xx.** Any handler behind an `hx-swap` that can return
  an error status must return a renderable body at 200, or the user sees nothing
  at all. This is the single most repeated root cause in the defect record.
- **CSP is `style-src 'self'` with no `unsafe-inline`.** Inline styles and
  htmx's own injected `<style>` are refused; it is configured off via htmx's own
  `includeIndicatorStyles: false` rather than by weakening the policy.
- **Go + Fiber + pongo2 + HTMX, no frontend framework.** Server-rendered
  partials. There is a class-coverage guard that fails if a template ships a
  class with no stylesheet rule — so adding a class is a two-file change.
- **`gofmt -w` must never be run on tracked CRLF files** in this working copy.

Explicitly undecided: whether the Jobs page should surface owner-less jobs to a
non-admin, and whether adoption semantics should ever apply to tables other than
libraries. Both are open product questions, tracked in Linear, not settled.

## Brand Commitments

Binding. The user confirmed the cyberpunk operations-console identity is part of
the product, not an artifact of an early pass: **restore it and extend it.**

- **Name:** NETRUNNER / Djinn NETRUNNER 2.0.
- **Established palette** — these tokens already exist in
  `ops/web/static/css/styles.css` and are the incumbent, not a suggestion:
  `--bg-primary: #0a0e14`, `--bg-secondary: #111820`, `--bg-tertiary: #1a2332`,
  `--bg-card: #0d1219`; `--text-primary: #e6edf3`, `--text-secondary: #8b949e`,
  `--text-muted: #484f58`; neon accents `--accent-cyan: #00fff5`,
  `--accent-magenta: #ff00ff`, `--accent-green: #00ff41`,
  `--accent-orange: #ff6b00`, `--accent-red: #ff0040`; `--border-glow:
  rgba(0, 255, 245, 0.3)`.
- **Voice:** an operations console, not a consumer app. Terse, specific, and
  honest about state. The product's defining voice trait is that it never claims
  to be doing something it is not — that is the standard every string is held to.
- **Aesthetic:** dark, neon accent on near-black. There is **no Bento grid** —
  the dashboard is a `.dashboard` container whose only grid is `.stats-region`
  (`repeat(auto-fit, minmax(200px, 1fr))`), plus a `.detail-grid` (`1fr 1fr`)
  inside modal bodies. There is **no `backdrop-filter` rule anywhere**; the
  only mention is a comment recording that it was deliberately avoided in favour
  of z-index layering. Treat "glassmorphic" as a description of intent, not of
  what renders.

## Evidence on Hand

- `docs/USERFLOW.md` — the first-session flow, driven through the real UI, with a
  "Known rough edges" section that names what still trips a new user.
- `docs/WHITEPAPER.md`, `docs/ARCHITECTURE.md`, `FEATURES.md`, `QUICKSTART.md`.
- `docs/BETA_ACCEPTANCE.md` and `docs/BETA_ACCEPTANCE_EVIDENCE.md` — measured
  evidence, the evidence record for release claims.
- `codemap.md` and `ops/web/templates/codemap.md`.
- A real seeded music library in the `netrunner-music` volume, holding actual
  audio for Audiotree, Boards of Canada, Broadcast, Chelsea Wolfe, Converge
  (three collaboration variants), Noriyuki Iwadare, Overcast, PUP, Pink Floyd
  and Various Artists — which is what makes browse, dedup and enrichment
  demonstrable rather than theoretical.
- Linear project **P-DJI-28** "NetRunner — User Playtest Defects" — 45 tickets
  observed by playing the real product, each with a slice and evidence. Five are
  closed as Duplicate or Canceled (they were not independent defects), so 40
  stand as real ones.

**Absences future work must not fabricate:**

- **No webfont is actually loaded.** `styles.css` names `Inter` and
  `JetBrains Mono`, and the README names `Orbitron`, but there is **no
  `@font-face` and no font link anywhere in the repository** — every one
  resolves to a system fallback. The typography in the committed brand is
  aspirational, not rendered. Any screenshot showing "Orbitron" is showing a
  fallback.
- **No logo or favicon asset exists.** Nothing in `ops/web/static/` beyond
  `css/`, `js/` and a codemap.
- **No customer logos, testimonials, benchmarks, pricing or licence claims.**
  There is no marketing surface to draw these from, and none should be invented.

## Product Principles

1. **Never claim what you are not doing.** If a control cannot report its own
   state, it does not ship looking like it can. This has outranked feature
   completeness repeatedly, and a passing test over an inert attribute is treated
   as a defect, not an achievement.
2. **Show the work, or stay silent.** Every action tells you what it did. An
   absence of feedback is a bug even when the action succeeded.
3. **The choice is the operator's.** When the system resolves ambiguity on the
   operator's behalf — which artist, which path, which candidate — it must let
   them choose. Silent defaults are how you end up monitoring the wrong band.
4. **Ownership is real and must stay real.** Scoping between operators is a
   correctness property, not a filter to be relaxed for convenience.
5. **Earn trust by being diagnosable.** A stuck job, a missing file and a failed
   search each need an explanation and a remedy inside the product.

## Accessibility & Inclusion

**WCAG 2.1 AA is the floor**, confirmed by the user, not an aspiration. Contrast,
visible focus, keyboard operability of every control, and correct
names/roles/values are acceptance criteria.

This has direct product consequences already visible in the code: `role="alert"`
on messages that must interrupt, `aria-live="polite"` on regions that update
under the user, `role="status"` on async replies, and `aria-labelledby` on
modals. Motion is handled in the stylesheet rather than the markup — there is a
`prefers-reduced-motion` block in `styles.css`, and no template references it.

The console is also the screen-reader path for diagnosing a live incident, so
log output cannot rely on colour or on visual-only "Jump to Error" affordances.
---
version: 1
slug: "ops-web-templates-partials-artist-form-html"
primary_target: "ops/web/templates/partials/artist-form.html"
related_targets: []
---

# Add Artist candidate picker

## Scope

Route: `POST /api/artists/search` (new, returns candidates) rendered into the
existing Add Artist modal on `/artists`. Visitor mode: **Operate**. This is a
state added inside an established surface; it inherits the incumbent world and
does not rewrite DESIGN.md.

## Audience, job, action

The single operator, adding an artist they can name but not disambiguate. Job:
monitor the artist they meant. Action: search, read the candidates, confirm one.
Proof/content: real MusicBrainz candidates — name, disambiguation, country, type.

Constraints carried from the ticket and the operator's decisions:

- The browser always chooses. There is no "use the top match" escape hatch.
- A single unambiguous result is still shown and confirmed, never auto-accepted.
- Zero results says so and creates no row. A failed or timed-out search says the
  *search* failed and offers retry — it is never reported as "not found".
- `POST /api/artists {"name":…}` keeps resolving the top match, so
  `docs/DEPLOYMENT.md`, the CLI and MCP callers keep working. The silent default
  is fixed for browsers; the API's behaviour stays documented, not silent-but-
  undocumented.

## Direction contract

**THESIS** — The choice is the operator's, and the surface exists to make five
same-named artists comparable at a glance. It refuses the category default of a
search box that resolves ambiguity behind your back.

**OWN-WORLD** — The incumbent NETRUNNER console: near-black `--bg-card` grounds,
neon cyan `--accent-cyan` on the selected affordance, `--text-secondary` for
disambiguating detail, 1px `--border-color` rows. Components are the existing
`.modal`, `.form-group`, `.btn`, `.notice` and `.text-secondary` — the picker
adds no new identity, it reuses the modal it already lives in.

**STORY** — I typed a name. The system found five things that answer to it. Now
I can see why they differ and pick one. After I pick, exactly that artist is
monitored, and the modal closes onto a list that shows it.

**FIRST VIEWPORT** — Unchanged: the Artists page with `#artists-region`. The
modal overlays it on Add. Modal header "Add Artist" persists across both states
so the operator never loses their place. State one is the name + quality profile
form. State two replaces the form body only: a short line naming what was
searched, then the candidate rows. Each row is one button, full width: artist
name at body weight on the left, then disambiguation / country / type as
secondary text on the same line; the whole row is the target, not a small link
at the end. The primary action is the row itself — there is no separate confirm
button.

**FORM** — Extension of an established surface; no concept-seed was run and no
seed key exists. Inherits the incumbent world per new-work §3 "Extend an
existing surface". The chosen structure was confirmed by the operator: one
modal, two states, always land on a list.

**SIGNATURE INTERACTION** — The state change is the whole event: the form
dissolves into the candidate list in place, `hx-target` scoped to the modal body
so the overlay never closes. No page navigation, no second screen.

**FINISH** — unreviewed and undocumented is unfinished; this build ends with the
finish review, the verdict, DESIGN.md, and every shipping raster carrying its
provenance

## Unresolved

- Whether a candidate already monitored by this operator is marked. `AddMonitoredArtist`
  is not reached until this slice, so a duplicate is possible; marking it is out
  of this ticket's scope and gets filed if it proves to matter live.

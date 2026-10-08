# DJI-630 — What does the artist-provenance browser coverage still lack?

Ticket: [DJI-617](https://linear.app/djinnet/issue/DJI-617/browser-coverage-for-artist-provenance-and-re-point-task-6) ("Browser coverage for artist provenance and re-point (Task 6)"), the last open item in its project.

**Verdict: still live, and worse than "uncovered".** Two gaps exist at browser level. One is a plain hole (re-point has never been driven in a browser). The other is a hole that a browser spec would **fail**, because the re-point response and the swap target that receives it disagree.

Everything below is established by reading the code; nothing was observed in a running browser (no terminal in this slice — see *Could not verify*).

## Existing coverage

### Playwright (`e2e/tests/`)

| Spec | Cases | What it pins |
|---|---|---|
| `artists.spec.ts` | `:24`–`:139`, 11 tests | page header, region load, empty state, Add modal opens, add form *searches* (`form[hx-post="/api/artists/search"]`), cancel, list API returns an array, 400 on a missing name, Sync 404, nav, unauthenticated refused. **No provenance assertion, no re-point anywhere.** |
| `artist-picker.spec.ts` | `:80` | The Add picker end to end: `POST /api/artists/search` then `POST /api/artists`, candidate rows render, the click issues the request, `.artist-card` appears, exactly one `artist_scan` job. Drives the MusicBrainz **stand-in**. |
| `artist-scan.spec.ts` | `:69`, `:89` | Seeded row + queued scan; test 2 is the **only** browser interaction with a card *button* today — `artistCard.getByRole('button', { name: /Sync discography for/ }).click()` then `#notice` reads "Sync already active". |
| `jobs.spec.ts` | `:84`–`:86` | `artist_scan` is one of the job types the Jobs page can render. |

A grep of `e2e/` for `repoint|re-point|Re-point` returns **zero** matches; for `country|artist_type|provenance` also **zero**. The only `page.request.patch` calls in the suite are libraries, playlists, schedules and watchlists.

### Go handler / template tests (already deep — the browser must not repeat these)

| File | What it pins |
|---|---|
| `artist_repoint_test.go` | 6 tests: counters reset and monitoring flags kept; owner-scoped **and not admin-exempt** (both roles); same-MBID is a no-op; an already-queued scan is answered, not duplicated; an unresolvable MBID leaves the row intact; **the response is the updated card** (`assert.Contains(t, body, "United Kingdom", ...)`, `assert.NotContains(t, body, "old-card-mbid", ...)`). |
| `artist_picker_repoint_test.go` | 6 tests: without `repoint_for` the row still posts to `/api/artists`; with it, `hx-post="/api/artists/artist-uuid-123/repoint"`; the `searchFailed` retry carries `repoint_for`; `noMatch` offers no retry; `noName` is not an outage; the card's Re-point control carries `"repoint_for"`. |
| `templates/artist_card_actions_test.go` | All four card actions bound to method + endpoint + `hx-target`: `{"Sync", "POST /api/artists/a1/sync", "#notice"}`, `{"Re-point", "POST /api/artists/search", "#modal-container"}`, `{"Pause", "PATCH /api/artists/a1", "#artist-a1"}`, `{"Remove", "DELETE /api/artists/a1", "#artist-a1"}`. |
| `artist_picker_test.go` | The four picker states, and that a confirmed pick is re-read from MusicBrainz by ID, not trusted from the form. |

## What is actually missing

### Gap 1 — the re-point journey has never been driven in a browser

Stated as assertions:

- **A1.** Clicking the card's `Re-point` button opens a modal whose title is `Re-point artist`. `artist-form.html` renders `{% if repoint_for %}Re-point artist{% else %}Add Artist{% endif %}`; nothing browser-level ever asserts the `repoint_for` branch is taken.
- **A2.** That click issued `POST /api/artists/search` carrying `repoint_for` set to *this card's* id — captured from the request, the way `artist-picker.spec.ts:88` captures `posts`. Today the only guard is `assert.Contains(t, body, "\"repoint_for\"")` in `artist_picker_repoint_test.go` — a **substring** check, and this repo has already shipped a defect that a substring check passes (a nested button whose markup contains the right text and whose click does nothing).
- **A3.** Choosing a candidate issues `PATCH /api/artists/<id>/repoint` and **not** `POST /api/artists`. This is the load-bearing one: `artist_picker_repoint_test.go` names the failure mode — "the add route would create a SECOND monitored artist instead of moving this one" — and only a browser proves which request the click makes.
- **A4.** Afterwards `GET /api/artists` has the **same number of rows** as before, and the card names the new MBID and the new entity's provenance.
- **A5.** No `pageerror` during the flow (the `js:`-values / `script-src 'self'` class that made the Add picker inert — the reason `artist-picker.spec.ts` exists).

### Gap 2 — the re-point response does not match the target that receives it

This is the finding that makes the missing spec a *failing* spec rather than just a hole.

The handler returns **one card**: `artist_repoint_handler.go:81`

```go
return c.Render("partials/artist-card", fiber.Map{"Artist": updated})
```

and its comment asserts the swap that goes with it:

> `// Return the updated card. The control that triggers this swaps`
> `// #artist-<id> with outerHTML, so a JSON body would leave the operator`
> `// looking at the provenance of the entity they just replaced.`

But the control that actually issues the PATCH is the **candidate row**, and it targets the whole region — `artist-candidates.html:90-92`:

```html
hx-post="/api/artists/{{ repoint_for }}/repoint"
hx-include="closest [role='listitem']"
hx-target="#artists-region"
hx-swap="innerHTML"
```

Compare the Add path, which uses the same target and returns the matching body: `artists.go:417`

```go
return c.Render("partials/artists", fiber.Map{"artists": artists})
```

`partials/artists.html` is the region — it carries `<div class="artists-region">`, the `.section-header` with the **Add Artist** button, and `#artists-list` holding every card. The card partial carries none of that.

So a one-card body swapped into `#artists-region` with `innerHTML` replaces the region's **entire** content. After a successful re-point:

- **A6.** Every *other* monitored artist's card is gone from the page.
- **A7.** The `.artists-region` wrapper is gone (it lives in the region partial, not in the page).
- **A8.** The `.section-header` — the "Monitored Artists" heading and the **Add Artist** button — is gone, so the operator cannot add an artist again without a reload.

`pages/artists.html:11` re-fetches the region on every load (`<section id="artists-region" hx-get="/partials/artists" hx-trigger="load">`), which is precisely why a reload hides this and no spec has caught it.

Why no existing test sees it: `TestArtistsHandler_RepointReturnsTheUpdatedCard` asserts the **response body** is the right card. It cannot assert what the browser swapped where. `artist-picker.spec.ts` does click a candidate into the same region target and passes — because it holds exactly **one** artist, so there is no second card to lose and the region partial's absence is invisible.

## Disposition

**Write one new spec, `e2e/tests/artist-repoint.spec.ts`** — then fix what A6–A8 assert. Do not close as covered, and do not fold this into `artist-picker.spec.ts`.

- A new spec rather than an extension, because A6–A8 need **two** artists in one view (re-point one, assert the other survives). `artist-picker.spec.ts` is written around a single `const ARTIST = 'Boards of Canada'` and a single `artistId`; expressing a second artist there means rewriting its fixture and its cleanup, which is a bigger change than adding a spec.
- The stand-in already supplies everything needed, so **no production machinery is required**: `ops/fake-musicbrainz/entrypoint.py:50-52` returns `"disambiguation": "Scottish duo", "country": "GB", "type": "Group"` for the picker fixture, and `entrypoint.py:153` serves the by-ID lookup the re-point handler resolves against (`m = re.fullmatch(r"/ws/2/artist/([^/]+)", path)`).
- Do **not** build the provenance fixture through `POST /api/test/seed-monitored-artist`: `testapi.go` calls `svc.AddMonitoredArtist(payload.MusicBrainzID, profileID, name, name, "", "", "", &user.ID)` — disambiguation, country and type are hard-coded empty, so a seeded artist renders the card's provenance span as absent (`artist-card.html:12` guards it with `{% if Artist.Disambiguation or Artist.Country or Artist.ArtistType %}`). Provenance must come from the picker, which is what the stand-in answers.
- A6–A8 are a **product bug to file**, not merely a spec to write. Either the candidate row should target `#artist-<id>` with `outerHTML` for the re-point branch (matching the handler's own comment), or `Repoint` should render `partials/artists` (matching Add). The first is the smaller change and is what the handler already claims.

## Could not verify

- **No browser was run.** This slice has no terminal, so nothing here was observed against a running stack. A6–A8 are derived from two things I did verify by reading: the render target in `artist_repoint_handler.go:81`, and the swap attributes on the candidate row in `artist-candidates.html:90-92`.
- Whether htmx resolves `#artists-region` to the page's `<section>` (and therefore replaces the region rather than nesting inside it) is read off `pages/artists.html:11`, not measured.
- I did not check whether any spec outside `e2e/` (a script, a mutation harness) drives re-point through a browser.

## What would falsify this verdict

- **Gap 1 falsified** if a grep for `repoint|re-point|Re-point` over `e2e/` (and any other browser harness) shows a spec that already clicks the button and picks a candidate.
- **Gap 2 falsified** if the re-point route the browser uses is not the one I read — i.e. if `hx-target` on the re-point branch of the candidate row resolves to something other than `pages/artists.html`'s `<section id="artists-region">`, or if `partials/artist-card.html` itself contains `.artists-region` and the `.section-header`. It does not (its root is `<div class="artist-card" id="artist-{{ Artist.ID }}">`).
- **Gap 2 downgraded** if `Repoint` is unreachable from the UI — but `artist-card.html:21-27` ships the button, `artist_picker_repoint_test.go` pins its `repoint_for`, and `main.go:365` registers `artistsRoutes.Patch("/:id/repoint", artistsHandler.Repoint)`.

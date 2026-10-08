# DJI-627 — Are the two artist-tracking defects still live?

Ticket: [DJI-627](https://linear.app/djinnet/issue/DJI-627). Covers DJI-605 and DJI-607.

Method: read the handlers, the service, the partials and the tests. Every assertion below quotes the code
it rests on. No terminal was available, so there is no `wc -l` — see "Could not verify".

## Claim A — DJI-605: adding an artist queues a scan and announces nothing, while Sync on the same page does announce

**Verdict: still live.**

### Evidence

The asymmetry is one handler each, and both are unchanged.

**Sync announces twice over.** `backend/internal/api/artists.go:340-353`:

```go
	if alreadyActive {
		c.Set("HX-Trigger", "sync-already-active")
		if isHTMXRequest(c) {
			return c.Type("html").SendString("<div class=\"scan-status\">Sync already active for artist " + html.EscapeString(artist.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
		}
...
	c.Set("HX-Trigger", "sync-queued")
	if isHTMXRequest(c) {
		return c.Type("html").SendString("<div class=\"scan-status\">Sync triggered for artist " + html.EscapeString(artist.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
	}
```

The button that calls it puts that body somewhere visible — `ops/web/templates/partials/artist-card.html:19`:

```html
<button class="btn btn-sm btn-secondary" hx-post="/api/artists/{{ Artist.ID }}/sync" hx-target="#notice" hx-disabled-elt="this" aria-label="Sync discography for {{ Artist.Name }}">
```

`#notice` is a live region — `ops/web/templates/layouts/base.html:58`:

```html
<div id="notice" class="notice" role="status" aria-live="polite"></div>
```

So a Sync swaps a `scan-status` message into a `role="status" aria-live="polite"` region, and also fires
`HX-Trigger: sync-queued`.

**Add announces nothing of the sort.** `backend/internal/api/artists.go:139-143`:

```go
	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderPartial(c)
	}
	return c.Status(201).JSON(monitored)
```

`closeModal` is the modal-close event only — consumed by `ops/web/static/js/app.js:234`
(`document.body.addEventListener('closeModal', function() {`) — and says nothing about a scan.

`RenderPartial` answers with `partials/artists`, i.e. the whole **region**, and the picker's confirmed row
swaps it into `#artists-region` — `ops/web/templates/partials/artist-candidates.html:86-90`:

```html
                {% if repoint_for %}hx-post="/api/artists/{{ repoint_for }}/repoint"{% else %}hx-post="/api/artists"{% endif %}
                hx-include="closest [role='listitem']"
                hx-target="#artists-region"
                hx-swap="innerHTML"
                hx-disabled-elt="this">
```

That body carries no `scan-status` element, and `#notice` is never written on this path. I read
`partials/artists.html` in full: it renders the section header, the Add button and the card list, and has
no announcement markup.

**The scan is nevertheless queued on Add.** `backend/internal/services/artist_tracking_service.go:87`,
inside `AddMonitoredArtist`'s transaction:

```go
		if _, _, err := s.queueArtistScan(tx, &artist, "user_api"); err != nil {
			return err
		}
```

So the defect is exactly as reported: the promise is made and kept, and the operator is not told it was
made. They get a new card, and nothing distinguishes a scanned artist from an unscanned one.

Two in-repo comments already agree with this reading and are worth citing when it is fixed:
`backend/internal/api/templates/ui_honesty_test.go:22` ("`#notice` stayed empty. The click read as doing
nothing while quietly") and `backend/internal/api/templates/htmx_target_coverage_test.go:101-103`
("layouts/base.html must declare #notice — the Sync buttons post into it, so without it those buttons go
inert on every page again").

### Disposition

**Fix now.** The plumbing already exists and is proven: write the same `scan-status` message into
`#notice`, and/or fire the event name the Sync path already uses, from the Add path. The decision recorded
in [DJI-609](https://linear.app/djinnet/issue/DJI-609) ratifies `#notice` as Sync's channel and records
"the Add path does not use it" as the follow-up — this ticket is that follow-up.

The cheapest assertion that would have caught it: a handler test asserting the Add response announces the
scan it just queued, mirroring `backend/internal/api/artists_test.go:62`
(`assert.Equal(t, "sync-queued", resp.Header.Get("HX-Trigger"))`).

## Claim B — DJI-607: ArtistTrackingService is ~400 lines owning row lifecycle, enqueue, discography sync and acquisition fan-out

**Verdict: still live as described — and it is not merely long: there is a nameable seam.**

### Current size

`backend/internal/services/artist_tracking_service.go` is **403 lines**: `RefreshArtistReleaseCounters` is
declared at `:379` and its final statement, ``WHERE id = ?`, artistID, artistID, artistID).Error``, is at
`:402`, with the closing `}` at `:403`. The ticket's "~400 lines" is accurate to within three.

### Responsibilities and the candidate seam

Four concerns in one type:

| Concern | Members |
|---|---|
| Row lifecycle (add / read / update / delete) | `AddMonitoredArtist` `:37`, `GetMonitoredArtists` `:148`, `UpdateArtistStatus` `:159`, `DeleteMonitoredArtist` `:168` |
| Enqueue ownership | `QueueArtistScan` `:112`, `queueArtistScan` `:116` |
| Discography scan + acquisition fan-out | `SyncDiscography` `:193` |
| Counter recomputation | `RefreshArtistReleaseCounters` `:379` |

The seam is the last two rows, and the signal is the **caller set**, not the line count.

`SyncDiscography` is reached from `backend/cmd/worker/main.go:1087`
(`err = w.atService.SyncDiscography(artistID)`, the `case "artist_scan":` branch) and from
`backend/internal/services/release_monitor_service.go:39` (`s.at.SyncDiscography(artist.ID)`) — **never
from an HTTP handler**, which is where every other method here is called from. It is also the only member
that reaches the network (via `s.mb.GetArtistDiscography`), creates `TrackedRelease` rows, and fans out
into an `acquisition` job with `JobItem` rows. One coherent unit, with its own lifecycle and its own caller
set.

`RefreshArtistReleaseCounters` has the same caller profile — `cmd/worker/main.go:1061` and
`SyncDiscography` at `:357` — and its own doc comment names exactly two callers: "the two places that
change the underlying truth: SyncDiscography at the end of a scan, and the worker's acquisition finaliser".

**Name the seam as: _the scan._** Extract `SyncDiscography` + `RefreshArtistReleaseCounters` and the
acquisition fan-out into their own type (a discography/scanner service), leaving `ArtistTrackingService`
owning row lifecycle and the enqueue.

That this is the codebase's own pattern is visible in three places, which is why the seam is nameable
rather than invented:

- `backend/internal/services/artist_repoint.go:38` — `RepointMonitoredArtist` has **already** been split
  out of this same type into its own file.
- `backend/internal/services/release_monitor_service.go:14-18` — a separate service already exists that
  holds `at *ArtistTrackingService` and drives the scan.
- `RefreshArtistReleaseCounters` is already a package-level func with its own suite
  (`backend/internal/services/artist_release_counters_test.go:61` onwards).

**Do not split `AddMonitoredArtist`'s two concerns.** Its row-insert + enqueue inside one transaction is
deliberate and documented in its own doc comment: "the row and its first scan are written in ONE
transaction". That atomicity *is* DJI-588's fix, not an accident.

### Disposition

**Refactor task, not a defect — and lower urgency than Claim A**, which is a real behaviour bug in the same
file. Land the seam as its own PR with no behaviour change (each concern already has its own tests), after
or independently of the DJI-605 fix. Do not bundle a move with a behaviour change.

## What would falsify this verdict

- **Claim A:** an `HX-Trigger` listener in `ops/web/static/js/app.js` (or an `hx-on` in a template) that
  renders a scan announcement for the Add path, or an Add response body carrying a `scan-status` element.
  I found neither: the Add path sets only `closeModal`, and the region it renders has no announcement
  markup. A test asserting the Add path announces would also falsify it.
- **Claim B:** a line count materially different from 403 (see below), or `SyncDiscography` having an HTTP
  caller that would put it in the same concern as the CRUD methods. Searches found only
  `cmd/worker/main.go:1087` and `release_monitor_service.go:39`.

## Could not verify

- **The exact line count by `wc -l`.** No terminal in this investigation. 403 is derived from
  line-numbered searches (`:379`, `:402`) plus a full read of the file; if the file lacks a trailing
  newline, `wc -l` will report 402. Either way the ticket's "~400" holds.
- **Whether `#notice` is visually prominent** — contrast, placement, auto-clear. `layouts/base.html:58`
  establishes only that the live region exists; it is CSS/JS behaviour beyond this question.
- **Runtime behaviour.** Both verdicts are from reading source. "Add announces nothing" was not exercised
  in a browser. `e2e/tests/artist-scan.spec.ts:89` covers the card and the queue but its assertions do not
  reach the announcement.

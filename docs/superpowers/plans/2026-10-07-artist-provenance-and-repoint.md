# Artist Provenance and Re-point — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A monitored artist's card shows which MusicBrainz entity it points at, and the operator can re-point a row at a different one without removing and re-adding it.

**Architecture:** Two columns (`country`, `artist_type`) on `monitored_artists`, populated from the `MusicBrainzArtist` the service already resolves at add time. A new `PATCH /api/artists/:id/repoint` reuses the existing candidate picker by threading an optional `repoint_for` through the search endpoint, so the same partial serves both flows. The card is de-duplicated into `artist-card.html` (D3) because DJI-589 changes both render sites.

**Tech Stack:** Go 1.25.13, Fiber, GORM, Pongo2, SQLite + PostgreSQL, Playwright (e2e only).

**Spec:** `docs/superpowers/specs/2026-10-07-artist-provenance-and-repoint-design.md` — read it first; this plan implements it and the two must not drift.

## Global Constraints

- **The `go` directive stays `1.25.13`.** Never let `go get`/`go mod tidy` raise it. CI pins go 1.25.13 and the Docker builder sets `GOTOOLCHAIN=local`.
- **Column is `artist_type`, never `type`.** Go field `ArtistType`, tag `gorm:"column:artist_type"`.
- **Re-point is NOT admin-exempt.** It takes `(id, userID)` and applies `Where("owner_user_id = ?", userID)` unconditionally. `UpdateArtistStatus` and `DeleteMonitoredArtist` both take `(id, userID, isAdmin)`; re-point deliberately omits the third.
- **Re-point resets `AcquiredReleases`, `TotalReleases`, `LastScanDate` to zero/nil (D4).** It **keeps** `Monitored`, `MonitorNew`, `MonitorAlbums`, `MonitorEPs`, `MonitorSingles`, `MonitorCompilations`, `MonitorLive`, `QualityProfileID`, `OwnerUserID`. It **does not** touch `TrackedRelease` rows.
- **Re-point re-reads the entity from MusicBrainz by ID** and never trusts a posted `name` — the row stored is the row MusicBrainz holds for that ID.
- **No new API calls.** `Add` and `repoint` both already hold a resolved `MusicBrainzArtist`.
- **Preserve each file's existing line endings.** Templates are CRLF; `artists.html` and `artist-card.html` must stay CRLF. Verify by byte probe (`py -c "d=open(f,'rb').read(); print(d.count(b'\r'), d.count(b'\n'))"`) after editing, not by reading the file back.
- **`gitleaks git --staged --redact` before every commit.**
- **Go tests on this host must clear ambient env:** `env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test …`. Otherwise `internal/config` fails three tests for reasons that have nothing to do with your change.
- **`internal/services` needs `-timeout 30m`**; it exceeds go test's 10m default when an ffprobe-backed test runs long.

## Review Focus

Five input classes the spec implies but no task's happy path exercises. Each has a test pinned to the task that owns the code.

1. **Re-pointing to the same MBID** — a no-op success. It must not wipe counters or re-queue a scan for the entity already being tracked.
2. **Re-point while a scan is already queued or running** — must answer the existing job, not queue a second. `queueArtistScan` already answers an active job; re-point must go through it, not `QueueArtistScan` on a fresh row.
3. **Another user re-points this row** — refused, and the victim's row is completely unmutated (provenance, counters, name).
4. **Partial provenance** — country set, type empty (MusicBrainz entities routinely have one and not the other). The card must render exactly one `·` separator, never `Name · GB ·`.
5. **The MBID no longer resolves** — during re-point, the handler must refuse and leave the row pointing at the old entity, rather than storing a blank entity.

---

### Task 1: Persist provenance on add

**Files:**
- Create: `ops/db/init/migrations/2026_10_07_001_artist_provenance.sql`
- Modify: `ops/db/init/migrations/codemap.md` (table row)
- Modify: `backend/internal/database/models.go` (add two fields to `MonitoredArtist`)
- Modify: `backend/internal/services/artist_tracking_service.go:37` (`AddMonitoredArtist`)
- Modify: `backend/internal/api/artists.go:133` (caller)
- Modify: `backend/internal/api/testapi/testapi.go:439` (caller)
- Test: `backend/internal/services/artist_tracking_service_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `AddMonitoredArtist(mbid string, qualityProfileID uuid.UUID, name, sortName, disambiguation, country, artistType string, ownerUserID *uint64) (*database.MonitoredArtist, error)`; `database.MonitoredArtist.Country string` and `.ArtistType string`. Tasks 3–4 read both.

- [ ] **Step 1: Write the failing test**

Add to `artist_tracking_service_test.go`:

```go
func TestAddMonitoredArtist_PersistsProvenance(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil, nil)

	got, err := svc.AddMonitoredArtist(
		"mbid-provenance", uuid.New(), "Napalm Death", "Napalm Death",
		"", "United Kingdom", "Group", nil,
	)
	require.NoError(t, err)
	require.Equal(t, "United Kingdom", got.Country)
	require.Equal(t, "Group", got.ArtistType)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/services -run TestAddMonitoredArtist_PersistsProvenance -v`
Expected: FAIL — `AddMonitoredArtist` accepts 5 arguments, not 8, so the call does not compile.

- [ ] **Step 3: Add the SQL migration**

Create `ops/db/init/migrations/2026_10_07_001_artist_provenance.sql`:

```sql
-- Provenance for a monitored artist: the two MusicBrainz fields that
-- disambiguate same-named entities. Decoded by the search service, discarded
-- by every writer until this column existed.
--
-- Idempotent, matching every other file in this directory.
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS country TEXT;
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS artist_type TEXT;
```

Add one row to the table in `codemap.md`, following the existing rows' shape.

- [ ] **Step 4: Add the model fields**

In `MonitoredArtist` (after `Disambiguation`):

```go
Country        string `gorm:"column:country" json:"country"`
ArtistType     string `gorm:"column:artist_type" json:"artist_type"`
```

- [ ] **Step 5: Widen `AddMonitoredArtist`**

Change the signature to take `disambiguation, country, artistType` between `sortName` and `ownerUserID`, and set them on the `database.MonitoredArtist` literal in the existing `tx.Create` block. Leave the duplicate check, the name fallback, the transaction, and the `queueArtistScan` call exactly as they are — the scan-enqueue promise and its rollback are already guarded by `artist_scan_queue_test.go`.

- [ ] **Step 6: Update both callers**

`artists.go:133` — the resolved `artist` value already carries the fields, so pass `artist.Disambiguation, artist.Country, artist.Type`. **Note the name change:** the service field is `Type`, the model field is `ArtistType`.

`testapi.go:439` — pass `"", "", ""`; the e2e seed endpoint has no MusicBrainz entity to read provenance from.

- [ ] **Step 7: Run the service tests to verify they pass**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -timeout 30m ./internal/services -run TestAddMonitoredArtist -v`
Expected: PASS, including the two pre-existing scan-enqueue and rollback tests.

- [ ] **Step 8: Run the api tests and commit**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -count=1 ./internal/api/...`
Expected: all `ok`.

```bash
git add ops/db/init/migrations/2026_10_07_001_artist_provenance.sql ops/db/init/migrations/codemap.md backend/internal/database/models.go backend/internal/services/artist_tracking_service.go backend/internal/api/artists.go backend/internal/api/testapi/testapi.go backend/internal/services/artist_tracking_service_test.go
gitleaks git --staged --redact
git commit -m "feat(artists): persist MusicBrainz country and type on the monitored row"
```

---

### Task 2: Render provenance on the card, and de-duplicate it

**Files:**
- Modify: `ops/web/templates/partials/artist-card.html`
- Modify: `ops/web/templates/partials/artists.html`
- Test: `backend/internal/api/artist_sync_template_test.go`

**Interfaces:**
- Consumes: `MonitoredArtist.Country` / `.ArtistType` (Task 1).
- Produces: one card partial, `artist-card.html`, rendering provenance. Task 4 adds the Re-point button to it; its shape must stay stable.

This is **D3** — the de-duplication. `artists.html` currently inlines a copy of the card using `artist.X`; `artist-card.html` holds the same markup with `Artist.X`. Replace the inline copy with an include so there is one source of truth.

- [ ] **Step 1: Write the failing test**

Add to `artist_sync_template_test.go`:

```go
func TestArtistCard_RendersProvenanceAndOmitsEmptySeparators(t *testing.T) {
	// Full provenance: all three present.
	//   -> "Napalm Death · United Kingdom · Group"
	// Partial (country set, type empty — Review Focus #4, and common on
	// MusicBrainz: plenty of entities have a country and no type):
	//   -> "Napalm Death · United Kingdom" and no trailing "·"
	// Nothing set (row predates the backfill):
	//   -> "Napalm Death" alone, and no bare "·" anywhere in the block.
}
```

Render `partials/artist-card.html` through the same harness the file's existing tests use, once per fixture. Assert on the rendered string: substring `"United Kingdom · Group"` for the full case, `"United Kingdom"` with no `"·"` after it for the partial case, and no `·` at all for the empty case.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/api -run TestArtistCard_RendersProvenance -v`
Expected: FAIL — the card renders no country or type today.

- [ ] **Step 3: Render provenance in `artist-card.html`**

Add one `<span>` to the existing `.details` div, after the MBID span. Join with `&middot;` **conditionally on each field**, so an absent field never leaves a separator:

```html
<span>
    {% if artist.Disambiguation %}{{ artist.Disambiguation }}{% endif %}
    {% if artist.Country %}{% if artist.Disambiguation %} &middot; {% endif %}{{ artist.Country }}{% endif %}
    {% if artist.ArtistType %}{% if artist.Disambiguation or artist.Country %} &middot; {% endif %}{{ artist.ArtistType }}{% endif %}
</span>
```

Wrap the whole span in `{% if artist.Disambiguation or artist.Country or artist.ArtistType %}` so a row with none renders no empty span. Copy this conditional-join shape from `artist-candidates.html`, which already does exactly this — the two must agree visually.

**Note the variable casing:** this file uses `Artist.X` (capital). Do not lower-case it.

- [ ] **Step 4: Replace the inline card in `artists.html` with the include**

Replace the whole `<div class="artist-card" …>…</div>` block inside the `{% for artist in artists %}` loop with:

```html
{% include "partials/artist-card.html" %}
```

The loop variable is `artist`, the partial expects `Artist`. Pongo2 `{# #}` comments cannot span lines — if you need a note, use one HTML comment line.

- [ ] **Step 5: Run the template guards**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -count=1 ./internal/api/templates ./internal/api -run 'Artist|Template|Escap|Partials|Heading' -v`
Expected: PASS. In particular `TestArtistSyncTemplate…` must still find the Sync button in both partials — **that test is what proves the include did not break the Pause/Resume swap target.** If it fails, the include is not receiving `Artist`.

- [ ] **Step 6: Verify CRLF and commit**

```bash
py -c "d=open('ops/web/templates/partials/artist-card.html','rb').read(); print(d.count(b'\r'), d.count(b'\n'))"
py -c "d=open('ops/web/templates/partials/artists.html','rb').read(); print(d.count(b'\r'), d.count(b'\n'))"
```
Expected: equal counts on both (CRLF preserved).

```bash
git add ops/web/templates/partials/artist-card.html ops/web/templates/partials/artists.html backend/internal/api/artist_sync_template_test.go
gitleaks git --staged --redact
git commit -m "fix(ui): show artist provenance, and render the card from one partial"
```

---

### Task 3: The re-point endpoint

**Files:**
- Create: `backend/internal/services/artist_repoint.go`
- Modify: `backend/internal/api/artists.go` (new handler `Repoint`)
- Modify: `backend/cmd/server/main.go:365` (route, beside the other artist routes)
- Test: `backend/internal/api/artists_test.go`

**Interfaces:**
- Consumes: `queueArtistScan` (internal, `artist_tracking_service.go`) — call it; do not re-implement. `mbService.GetArtist(mbid)`.
- Produces: `(*ArtistTrackingService).RepointMonitoredArtist(id uuid.UUID, mbid, name, sortName, disambiguation, country, artistType string, userID uint64) error` and `(*ArtistsHandler).Repoint`. Task 4 depends on the route existing.

- [ ] **Step 1: Write the failing tests**

Four tests in `artists_test.go`, all against the real handler:

```go
// 1. Happy path: swaps every identity field and resets the counters.
func TestArtistsHandler_RepointResetsCountersAndKeepsFlags(t *testing.T)
//    seed a monitored row with AcquiredReleases 12, TotalReleases 48,
//    LastScanDate set, MonitorEPs false
//    re-point to a different MBID
//    assert: MusicBrainzID/Name/SortName/Country/ArtistType are the new entity's
//    assert: AcquiredReleases == 0 && TotalReleases == 0 && LastScanDate == nil
//    assert: MonitorEPs is STILL false  (flags survive D4)

// 2. Review Focus #3 — another user cannot re-point, and nothing mutates.
func TestArtistsHandler_RepointIsOwnerScopedAndNotAdminExempt(t *testing.T)
//    seed a row owned by user A; re-point as user B (and as an ADMIN) -> refused
//    assert: status is 404 or 403
//    assert: the row still carries user A's original MBID and counters

// 3. Review Focus #1 — re-pointing to the same MBID is a no-op success.
func TestArtistsHandler_RepointToSameEntityPreservesCounters(t *testing.T)

// 4. Review Focus #2 — no second scan is queued when one is already active.
func TestArtistsHandler_RepointAnswersAnAlreadyQueuedScan(t *testing.T)
```

For test 4, seed a `running` job with `scope_type`/`scope_id` for that artist before re-pointing, then assert the job count for that scope is unchanged.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/api -run TestArtistsHandler_Repoint -v`
Expected: FAIL — no `Repoint` handler and no route.

- [ ] **Step 3: Implement `RepointMonitoredArtist` in the new file**

```go
func (s *ArtistTrackingService) RepointMonitoredArtist(
    id uuid.UUID, mbid, name, sortName, disambiguation, country, artistType string,
    userID uint64,
) error
```

Inside one `s.db.Transaction`:

1. Load the row with `Where("id = ? AND owner_user_id = ?", id, userID).First(&artist)`. **No `isAdmin` parameter and no admin branch** — see Global Constraints. `gorm.ErrRecordNotFound` returns a not-found error.
2. If `artist.MusicBrainzID == mbid`, return `nil` unchanged (Review Focus #1) — do not reset counters, do not queue a scan.
3. Update the identity fields and set `acquired_releases = 0`, `total_releases = 0`, `last_scan_date = NULL`. Do **not** touch the `monitor_*` flags, `quality_profile_id` or `owner_user_id`. Do **not** touch `TrackedRelease`.
4. `s.queueArtistScan(tx, &artist, "user_api")` — the internal one, so an already-active job is answered rather than duplicated (Review Focus #2).

- [ ] **Step 4: Implement the handler**

`(*ArtistsHandler).Repoint` in `artists.go`:

1. `currentUserFromLocals(c)` → 401 if absent.
2. `uuid.Parse(c.Params("id"))` → 400 on failure.
3. Body struct `{ MusicBrainzID string \`json:"musicbrainz_id" form:"musicbrainz_id"\` }`.
4. **`mbService.GetArtist(payload.MusicBrainzID)`** — resolve the entity here, exactly as `Add` does. **Never** persist a posted `name`. If `GetArtist` fails, return 400/502 and **leave the row untouched** (Review Focus #5).
5. Call `RepointMonitoredArtist` with the resolved `artist.ID/Name/SortName/Disambiguation/Country/Type`.
6. Return the updated card: render `partials/artist-card.html` with the row re-read from the DB. Match what `Delete`/`Update` return so the htmx swap target stays `#artist-{ID}` / `outerHTML`.

- [ ] **Step 5: Register the route**

In `main.go` beside line 365, keeping the artist group together:

```go
artistsRoutes.Patch("/:id/repoint", artistsHandler.Repoint)
```

**It must be registered after `Patch("/:id")`** only for readability — Fiber routes on distinct paths, but keep the ordering conventional so the group reads top-down.

- [ ] **Step 6: Run the api tests to verify they pass**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -count=1 ./internal/api -v -run TestArtistsHandler_Repoint`
Expected: PASS, all four.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/services/artist_repoint.go backend/internal/api/artists.go backend/cmd/server/main.go backend/internal/api/artists_test.go
gitleaks git --staged --redact
git commit -m "feat(artists): re-point a monitored artist at a different entity"
```

---

### Task 4: Give the picker a destination

**Files:**
- Modify: `backend/internal/api/artists.go` (`Search`, `renderCandidates`)
- Modify: `ops/web/templates/partials/artist-candidates.html`
- Modify: `ops/web/templates/partials/artist-card.html` (Re-point button)
- Modify: `ops/web/templates/partials/artist-form.html` (modal header)
- Test: `backend/internal/api/artist_picker_test.go`

**Interfaces:**
- Consumes: `PATCH /api/artists/:id/repoint` (Task 3), the card partial (Task 2).
- Produces: `Search` and `renderCandidates` accept an optional `repointFor string`. Nothing downstream consumes this task's output.

- [ ] **Step 1: Write the failing tests**

In `artist_picker_test.go`:

```go
// Without repoint_for, the candidate row still posts to /api/artists.
func TestArtistCandidates_WithoutRepointForPostsToAdd(t *testing.T)
// With repoint_for set, the row posts to /api/artists/{id}/repoint.
func TestArtistCandidates_WithRepointForPostsToRepoint(t *testing.T)
// The re-point form carries repoint_for into the search that opens the picker.
func TestArtistSearch_PassesRepointForThroughToThePicker(t *testing.T)
```

Assert on the rendered `partials/artist-candidates` HTML: the `hx-post` value contains `/repoint` (or equals `/api/artists`).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/api -run 'ArtistCandidates|ArtistSearch_PassesRepoint' -v`
Expected: FAIL — no `repoint_for` is read and the row's `hx-post` is hardcoded `/api/artists`.

- [ ] **Step 3: Thread `repoint_for` through the handler**

In `Search`, add `RepointFor string \`json:"repoint_for" form:"repoint_for"\`` to the payload struct, trim it, and pass it to every `renderCandidates` call — all four branches (`errNoSearchName`, `searchFailed`, `noMatch`, candidates).

Change the helper's signature to `renderCandidates(c *fiber.Ctx, name, profileID, repointFor string, candidates []services.MusicBrainzArtist, searchErr error) error` and add `"repoint_for": repointFor` to each of the four `fiber.Map`s.

- [ ] **Step 4: Render the destination on the candidate row**

In `artist-candidates.html`, the candidate row's button currently hardcodes `hx-post="/api/artists"`. Change to:

```html
{% if repoint_for %}hx-post="/api/artists/{{ repoint_for }}/repoint"{% else %}hx-post="/api/artists"{% endif %}
```

**Every other state branch (`noName`, `searchFailed`, `noMatch`) is untouched** — the pick only happens in the candidates state, so this is the one place a destination exists. The `searchFailed` branch's `retryEndpoint` stays `/api/artists/search`; the retry must preserve `repoint_for`, which it does automatically because the retry posts the form values collected by `hx-include`.

- [ ] **Step 5: Add the Re-point button to the card**

In `artist-card.html`'s `.actions` div, between Sync and Pause:

```html
<button class="btn btn-sm" hx-post="/api/artists/search"
        hx-vals='{"repoint_for": "{{ Artist.ID }}"}'
        hx-target="#modal-container" hx-swap="innerHTML"
        aria-label="Re-point {{ Artist.Name }} at a different artist">
    Re-point
</button>
```

It posts to the **existing** search endpoint with `repoint_for` set — there is no
new route for opening the picker. That is the whole point of threading the
destination: the same `Search` handler and the same partial serve both flows.

`hx-vals` with a JSON object is safe here: this app's CSP forbids inline
`<script>`, not `hx-vals`, and this is static markup rather than a `js:`
expression — **do not** use `js:`, which htmx compiles with `eval` and which is
exactly what broke the picker before (#335).

- [ ] **Step 6: Label the modal for the re-point case**

In `artist-form.html`, the header currently reads "Add Artist" unconditionally. Render "Re-point artist" when `repoint_for` is set so the operator never loses their place — the picker already keeps its own header stable for the same reason.

- [ ] **Step 7: Verify, then commit**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -count=1 ./internal/api/... ./internal/api/templates`
Expected: all `ok`.

Byte-probe both templates for CRLF, then:

```bash
git add backend/internal/api/artists.go ops/web/templates/partials/artist-candidates.html ops/web/templates/partials/artist-card.html ops/web/templates/partials/artist-form.html backend/internal/api/artist_picker_test.go
gitleaks git --staged --redact
git commit -m "feat(artists): let the candidate picker target a re-point"
```

---

### Task 5: Backfill existing rows

**Files:**
- Modify: `backend/cmd/cli/` — the `library` command group
- Test: `backend/cmd/cli/` — its existing test file for that group

**Interfaces:**
- Consumes: `mbService.GetArtist(mbid)`; the MusicBrainz client interface already injected into the CLI.
- Produces: `netrunner-cli library backfill-artist-provenance [--dry-run]`. Operator tooling; no other task depends on it.

**This is separable** — if the operator tooling proves awkward, land Tasks 1–4 and file the backfill as its own ticket. The feature is complete without it; existing rows simply render name-only until it runs.

- [ ] **Step 1: Write the failing test**

Using the existing `setupTestDB` helper in that package (note: `cmd/cli` tests swap package globals `db`, `cfg`, `jsonOutput`, `osExit` — **stub `osExit` before any test that can reach `handleError`**, or the real `os.Exit` kills the test binary mid-run):

```go
// --dry-run reports the rows it would fix and writes nothing.
func TestLibraryBackfillArtistProvenance_DryRunWritesNothing(t *testing.T)
// Rows whose MBID does not resolve are counted and reported, not silently skipped,
// and the command does not exit zero over them without saying so.
func TestLibraryBackfillArtistProvenance_ReportsUnresolvableRows(t *testing.T)
// A resolved row gets country and artist_type from the MB response.
func TestLibraryBackfillArtistProvenance_FillsProvenanceFromMusicBrainz(t *testing.T)
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./cmd/cli -run TestLibraryBackfillArtistProvenance -v`
Expected: FAIL — the subcommand does not exist.

- [ ] **Step 3: Implement the subcommand**

Follow the shape of the existing `library` subcommands (`list|add|scan|prune|rm`). It selects monitored artists whose `country` or `artist_type` is empty, calls `GetArtist` per row, writes the two fields, and honours `--dry-run`. **Rate-limit the MusicBrainz calls** — a large library is hundreds of rows against a rate-limited public API.

Report three numbers on completion: filled, unresolvable, already complete. **Exit non-zero if any row was unresolvable**, so a cron or script notices, and say which MBIDs failed.

- [ ] **Step 4: Run the tests, then commit**

Run: `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test -count=1 ./cmd/cli -v -run TestLibraryBackfillArtistProvenance`
Expected: PASS.

```bash
git add backend/cmd/cli/
gitleaks git --staged --redact
git commit -m "feat(cli): backfill artist provenance from MusicBrainz"
```

---

## Definition of done

- `cd backend && env -u ENVIRONMENT -u CONFIG_ENV -u PORT go vet ./...` clean, and `go vet -tags integration ./...` clean (the integration-tagged files never compile under a plain `go test ./...`).
- `go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent` succeeds.
- `go test -timeout 30m ./...` green on SQLite.
- Both driver paths validated for the migration — SQLite and PostgreSQL, not one assumed from the other.
- `scripts/e2e_gate.sh` still passes: `bash scripts/test_e2e_gate.sh`.
- The card shows provenance, one partial renders it, and re-point works end to end against a live stack.
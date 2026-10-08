# Artist Provenance and Re-point — Design

- **Date:** 2026-10-07
- **Ticket:** DJI-589 (third fix item on DJI-537, not delivered by DJI-548)
- **Path:** architectural (schema change; new route; two render sites)
- **Stage:** Release-readiness initiative, Stage 2 item 3

## 1. The problem

A monitored artist's card shows a name, an MBID, and release counts. Nothing on
it says *which* MusicBrainz entity is being monitored. Two consequences:

1. **Rows that already exist cannot be corrected.** DJI-548 fixed the choice at
   creation time, which protects new rows. It did nothing for rows the old
   silent-first-result behaviour created — DJI-548 proved one: `Napalm Death`,
   created when the operator typed "Death". Nothing on its card says so, and the
   only remedy is Remove and re-add, which loses the monitoring flags and
   counters.
2. **Provenance is decoded and thrown away.** `musicbrainz_service.go` decodes
   `Country` and `Type` — the two fields MusicBrainz itself uses to separate
   same-named artists — uses them in the picker, then discards them.
   `AddMonitoredArtist(artist.ID, profileID, artist.Name, artist.SortName, &user.ID)`
   persists neither.

So the disambiguation the product needs at the moment of choosing is unavailable
at the moment of checking, which is the only moment it is actually needed.

## 2. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | **Schema change** — add `Country` and `Type` to `monitored_artists` | The ticket's framing ("from the data the service already decodes") implies no schema change. That is wrong: the fields are not persisted. Re-probed 2026-10-07 — `MonitoredArtist` has `Disambiguation` but no `Country`/`Type` column. |
| D2 | **Persist**, not fetch-at-render | The artists list renders on every dashboard view. Fetching per card means N MusicBrainz calls against a rate-limited public API, making the page hostage to a third party's latency. A blank field is a better failure mode than a slow or absent page. |
| D3 | **Fold in the DJI-606 de-duplication** | *Reversible — flag for review.* DJI-589 must change both card render sites. "Keep DJI-606 separate" would mean writing the same new markup twice, which is precisely how the drift happened. One card partial, both call sites render it. |
| D4 | **Re-point resets the counters** | *Reversible — flag for review.* `AcquiredReleases`, `TotalReleases` and `LastScanDate` describe the *previous* entity. Carrying them across would report the old artist's discography as the new artist's. Reset to zero, then queue a scan. |

D3 and D4 are the two calls I made without an explicit answer. Both are cheap to
reverse in review; neither is a safety question.

## 3. Schema

```sql
-- ops/db/init/migrations/2026_10_07_001_artist_provenance.sql
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS country TEXT;
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS artist_type TEXT;
```

Column named `artist_type`, not `type`: `type` is a loaded word in SQL dialects
and buys nothing here, while the Go field is `ArtistType` and the decoded source
field is `Type`.

`database.Migrate` handles both drivers; this is a plain additive column so it
needs no enum-to-text conversion. **SQLite must be validated too**, not just
Postgres — `ADD COLUMN IF NOT EXISTS` is supported by both, but the migration is
run through the repo's own migration path rather than assumed.

Model:

```go
Disambiguation string
Country        string `json:"country"`
ArtistType     string `gorm:"column:artist_type" json:"artist_type"`
```

## 4. Service and API

**`AddMonitoredArtist`** gains the two fields. It already receives the resolved
`MusicBrainzArtist` at the call site, so no extra API call is introduced:

```go
func (s *ArtistTrackingService) AddMonitoredArtist(
    mbid string, qualityProfileID uuid.UUID,
    name, sortName, disambiguation, country, artistType string,
    ownerUserID *uint64,
) (*database.MonitoredArtist, error)
```

Two non-test callers: `artists.go:133` and `testapi.go:439`.

**New route — `PATCH /api/artists/:id/repoint`.** Distinct from
`PATCH /api/artists/:id`, which is the pause/resume toggle and must stay that.

Ownership follows the pattern the sibling routes already use, and it is
**not** admin-exempt. `UpdateArtistStatus` and `DeleteMonitoredArtist` both take
`(id, userID, isAdmin)` and apply `Where("owner_user_id = ?", userID)` unless
`isAdmin`. Re-point takes the same two arguments but **ignores `isAdmin`**: an
admin may pause or delete anyone's monitored row as an operator action, but
silently re-pointing someone else's row at a different artist rewrites what
their dashboard claims to be tracking. Unlike a quality profile there is no
global fallback that makes a bypass correct here — this is the BOLA shape #285
just fixed on `acquire`, and #285's own test is the pattern to copy.

Body: `musicbrainz_id` (+ `name`). The handler re-reads the entity from
MusicBrainz by ID rather than trusting the posted name, exactly as `Add` does —
the row stored is the row MusicBrainz holds for that ID.

On success: replace `MusicBrainzID`, `Name`, `SortName`, `Disambiguation`,
`Country`, `ArtistType`; **zero** `AcquiredReleases`, `TotalReleases`,
`LastScanDate`; queue a scan (D4 — the same enqueue `AddMonitoredArtist` already
performs, per DJI-588); leave `TrackedRelease` rows and library tracks alone.
Releases already acquired belong to the library and are not the monitor's to
delete.

## 5. UI

**Search must know its destination.** `POST /api/artists/search` gains an
optional `repoint_for` (the monitored artist's ID). It is threaded into
`renderCandidates` and rendered into each candidate row, so the same picker
partial serves both flows:

- no `repoint_for` → row posts to `/api/artists` (today's behaviour)
- `repoint_for` set → row posts to `/api/artists/{repoint_for}/repoint`

The row markup is otherwise **byte-identical**. One partial, one set of states
(`noName` / `searchFailed` / `noMatch` / candidates), two destinations — so the
four-state contract the picker already documents is unchanged.

**The card** gains provenance and one control:

```
Napalm Death · United Kingdom · Group
MBID: <mbid>   Releases: 12/48   Last Scan: 3 Oct 2026
[Sync] [Re-point] [Pause] [Remove]
```

Provenance renders only when present, joined with `·` — a row backfilled from a
deleted MusicBrainz entity shows the name and nothing else, which is honest.

`Re-point` opens the picker with `repoint_for` set, and the modal header reads
"Re-point artist" rather than "Add Artist" so the operator never loses their
place.

**De-duplication (D3).** `artists.html` and `artist-card.html` currently hold
copies of the card with different variable casing (`artist.X` vs `Artist.X`).
The card body moves into `artist-card.html`; `artists.html` iterates
`{% include "partials/artist-card.html" %}`.

Note pongo2 `{# #}` comments cannot span lines.

## 6. Backfill

Existing rows have empty `Country`/`ArtistType` and would render name-only until
re-added. Backfill by MBID through the same `GetArtist` the picker uses, with
rate limiting, as a **CLI subcommand rather than a migration** — a migration
must not make network calls, and this one can legitimately fail without blocking
a deploy.

It belongs on `netrunner-cli`, which already carries `library list|add|scan|prune|rm`,
because the codemap invariant is that core operations are exposed via both the
CLI and the MCP server. A `library backfill-artist-provenance` subcommand with a
`--dry-run` and a resume cursor satisfies that and is testable offline with a
fixture MB service; `scripts/` is the wrong home because everything there is a CI
or dev helper with no database access.

Rows whose MBID no longer resolves are left blank on purpose: there is no
provenance to record, and inventing one would be worse than showing nothing. The
command reports how many rows it could not resolve rather than exiting zero over
them.

## 7. Testing

- **Provenance renders.** Guard that the card shows disambiguation, country and
  type when set, and omits the separator cleanly when not. The ticket notes no
  such assertion exists today.
- **Re-point is owner-scoped.** Another user cannot re-point a monitored row —
  the BOLA test #285 established as the pattern.
- **Re-point resets counters** and leaves `TrackedRelease` rows intact.
- **Re-point re-reads from MusicBrainz** rather than trusting the posted name.
- **`AddMonitoredArtist` persists the two fields**, including the existing
  DJI-588 "queues the scan it promises" and rollback assertions, which must keep
  passing with the widened signature.
- **The de-duplication actually de-duplicates** — extend the existing drift test
  rather than adding a second agreement test.
- Both driver paths for the migration.

## 8. Risks

| Risk | Mitigation |
| --- | --- |
| Widening `AddMonitoredArtist` breaks callers/tests | Two non-test callers; mechanical. Compiler finds them. |
| Pongo2 include changes the swap target | `artist_sync_template_test.go` already asserts Sync exists in both partials — it will fail loudly if the include breaks it. |
| Re-point silently discards monitoring settings | Flags (`MonitorAlbums` etc.) are deliberately **kept**; only counters and scan timestamps reset. |
| MusicBrainz rate limit during backfill | One-shot, rate-limited, resumable; failure leaves blanks, which render honestly. |

## 9. Not in scope

DJI-605 (add-artist announces nothing), DJI-606 as a separate ticket (folded in
per D3), DJI-607 (`ArtistTrackingService` decomposition), DJI-601 (deleting an
artist strands its scan job). All four touch this same subsystem and are listed
in DJI-589's favour — land this first, then one context load for the rest.
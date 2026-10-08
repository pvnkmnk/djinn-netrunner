# DJI-625 — Are the three jobs-and-schema integrity defects still live?

Researched against the working tree. Every assertion is quoted from source with a
`path:line`. Anything I could not establish from the repo is under **Could not verify**
and is not load-bearing for a verdict.

---

## Claim A — DJI-583: owner-less jobs are silently omitted from the Jobs page

**Verdict: still live.**

The Jobs **page region** filters by owner for every non-admin, and `owner_user_id = ?`
never matches SQL `NULL`, so an owner-less row is excluded rather than shown:

`backend/internal/api/partials.go:123-125`
```go
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
```

The JSON API takes the same shape, so both surfaces agree:

`backend/cmd/server/main.go:428-430`
```go
		if user.Role != "admin" {
			q = q.Where("owner_user_id = ?", user.ID)
		}
```

The template that renders the region carries **no wording about scope**; the only empty
state is generic, so a viewer cannot tell a short list from a filtered one:

`ops/web/templates/partials/jobs.html:95-96`
```html
    {% empty %}
    <p class="empty-state">No jobs found</p>
```

That ownerless rows genuinely exist is not speculation — the sibling handler names them
as a real producer of unowned rows:

`backend/internal/api/jobs.go:35-36`
```go
// somebody else — or by one of the scheduler's ownerless release_monitor rows,
// which no user owns at all — otherwise has no in-product remedy, and the
```

A non-admin is then refused both the action and the logs on such a job:

`backend/internal/api/jobs.go:42-46`
```go
func mayActOnJob(user database.User, job database.Job) bool {
	if user.Role == "admin" {
		return true
	}
	return job.OwnerUserID != nil && *job.OwnerUserID == user.ID
}
```

`backend/internal/api/partials.go:82`
```go
	if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
```

**Admins are unaffected** — both filters sit inside `if user.Role != "admin"`, so an admin
gets the unfiltered list. The defect is scoped to non-admins, and it is the non-admin who
has no remedy.

**Disposition: fix now.** The silence is what makes this a defect rather than a policy.
Smallest sufficient fix: render the viewer's scope plus a count of the rows it excludes.
Costlier alternative: show owner-less rows to everyone as read-only. Either way the
assertion belongs on the *rendered region*, not the query — the defect is one of
disclosure.

---

## Claim B — DJI-601: deleting a monitored artist strands its queued artist_scan

**Verdict: still live.**

`DeleteMonitoredArtist` deletes the artist's `tracked_releases` then the artist row, and
nothing else — no cancellation or deletion of the job the add path queued:

`backend/internal/services/artist_tracking_service.go:180-189`
```go
	return s.db.Transaction(func(tx *gorm.DB) error {
		scope := tx.Model(&database.MonitoredArtist{}).Where("id = ?", id)
		if !isAdmin {
			scope = scope.Where("owner_user_id = ?", userID)
		}
		if err := tx.Where("artist_id IN (?)", scope.Select("id")).Delete(&database.TrackedRelease{}).Error; err != nil {
			return err
		}
		return scope.Delete(&database.MonitoredArtist{}).Error
	})
```

The worker's `artist_scan` branch resolves the artist by the job's scope id:

`backend/cmd/worker/main.go:1085-1087`
```go
	case "artist_scan":
		artistID, _ := uuid.Parse(jc.job.ScopeID)
		err = w.atService.SyncDiscography(artistID)
```

`SyncDiscography`'s first statement loads that same row, so a deleted artist returns
`gorm.ErrRecordNotFound`, whose `Error()` string is exactly `record not found`:

`backend/internal/services/artist_tracking_service.go:193-197`
```go
func (s *ArtistTrackingService) SyncDiscography(artistID uuid.UUID) error {
	var artist database.MonitoredArtist
	if err := s.db.Preload("QualityProfile").First(&artist, "id = ?", artistID).Error; err != nil {
		return err
	}
```

The error is returned unchanged and the monolithic finaliser turns any error into a
terminal `failed` whose summary is the error text — the card reads `failed` /
`record not found`, matching the ticket's wording:

`backend/cmd/worker/main.go:1242-1245`
```go
	if err != nil {
		finalState = "failed"
		summary = err.Error()
	}
```

This corroborates the decision note the coordinator holds: the delete cleans up
`tracked_releases` but not the scan job it queued.

**Disposition: fix now**, in `DeleteMonitoredArtist` — cancel the artist's queued/running
`artist_scan` inside the same transaction, so "delete an artist" and "leave nothing of it
behind" are one operation. Do **not** fix it by treating `ErrRecordNotFound` as success in
the worker: that would swallow a genuinely missing artist for every caller.

---

## Claim C — DJI-618: database.Migrate and the SQL bootstrap disagree about the schema

Two claims with two different answers.

### C1 — the `jobs.state` enum conversion

**Verdict: mis-stated.** Both paths convert it; there is no disagreement here.

`backend/internal/database/migrate.go:98-101`
```go
		// Step 3: Convert remaining ENUM columns to text (idempotent if already text or gone).
		for _, m := range []struct{ table, column, enumType string }{
			{"jobs", "state", "jobstate"},
			{"jobitems", "status", "jobitemstatus"},
```

`ops/db/init/migrations/2026_03_22_002_convert_enums_to_text.sql:30-31`
```sql
-- 5. Convert jobs.state from jobstate ENUM to text.
ALTER TABLE jobs ALTER COLUMN state TYPE text USING state::text;
```

Both are guarded (the Go side skips when the enum type is gone), so the two paths
converge on `text` rather than fighting.

**Disposition: close this half as already handled** — no code change; correct the
premise instead of acting on it.

### C2 — `MonitorEPs`' column name

**Verdict: still live.**

The model gives the field no column name, only a default:

`backend/internal/database/models.go:133-134`
```go
	MonitorAlbums       bool `gorm:"default:true"`
	MonitorEPs          bool `gorm:"default:true"`
```

The connection sets **no** `NamingStrategy` and no `NameReplacer`, so GORM's default
strategy names the column:

`backend/internal/database/connection.go:33-35`
```go
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
```

GORM's default strategy splits a capital run before a trailing lower-case letter, so
`MonitorEPs` → **`monitor_e_ps`**. Siblings are unaffected (`MonitorAlbums` →
`monitor_albums`) and `MonitorNew` is pinned explicitly via `column:monitor_new_releases`;
`MonitorEPs` is the one field where the derived and SQL names part company.

The SQL bootstrap creates the other spelling:

`ops/db/init/migrations/2026_03_04_006_artist_tracking.sql:38`
```sql
    monitor_eps BOOLEAN NOT NULL DEFAULT TRUE,
```

Not theoretical: a test must name the GORM-derived column to set the flag it claims to
exercise, and its comment says why.

`backend/internal/api/artist_repoint_test.go:101-105`
```go
	// MonitorEPs carries gorm:"default:true", so Create with false omits the
	// column and the database default wins. Set it explicitly, or the flag
	// this test claims to preserve was never the value it seeded.
	require.NoError(t, db.Model(&database.MonitoredArtist{}).
		Where("id = ?", artist.ID).Update("monitor_e_ps", false).Error)
```

On a SQL-bootstrapped Postgres, `AutoMigrate` is additive: it adds `monitor_e_ps` and
leaves `monitor_eps` as a **dead column**. Every read and write then uses `monitor_e_ps`
(including `artist_tracking_service.go:242`, `shouldMonitor = artist.MonitorEPs`), while
the schema's `monitor_eps NOT NULL DEFAULT TRUE` describes a column nothing reads. A row
seeded via SQL holds `NULL` in the column the app reads, and `NULL` scanned into a
non-pointer `bool` is `false` — so the SQL's `TRUE` default is not what the app observes.

**Disposition: fix now; treat the Go model as authoritative.** It is what runs, and a test
already writes the derived name. Minimal honest fix: stop leaving the name implicit — pin
`column:monitor_eps` on the model to adopt the schema's name, or leave the derived name and
align the SQL — plus a migration that reconciles the existing column rather than assuming
either, since a deployed database may hold both. Then close C1.

---

## Do any share a root cause?

- **A and B do not collapse.** A is a disclosure defect in a read path; B is a missing
  side effect in a write path. No single PR is the fix for both.
- **B is the second half of the already-ratified delete change.** Ratification made
  `DeleteMonitoredArtist` clean up `tracked_releases`; B is the queued job still not
  cleaned up. Fixing B there keeps both halves in one place, as the ratification asked.
- **C1 and C2 share a file and a theme, not a cause — do not bundle them.** Bundling
  would let a real fix (C2) be closed by a correction to a premise (C1).

## Which side is authoritative (C2)

**The Go model / GORM-derived `monitor_e_ps`**, because:

1. It is the name the running application reads and writes, so it decides behaviour.
2. `artist_repoint_test.go:105` already writes it by that name.
3. `AutoMigrate` runs at every startup and is additive, so `monitor_e_ps` exists on any
   database the app has ever opened; the SQL file can only describe a fresh volume.
4. Therefore `monitor_eps` is the outlier and the side to change — though pinning the
   model's name explicitly is safer than editing the SQL alone, since it makes the intent
   legible and stops the next field with a capital run repeating this.

## What would falsify these verdicts

- **A:** any template/partial/JS line stating the list is owner-scoped, or a query that
  unions owner-less rows in for non-admins.
- **B:** a DELETE/cancel of the scoped job inside `DeleteMonitoredArtist`, or a worker
  guard treating a missing artist as a no-op success.
- **C1:** a `Migrate` path that skips `jobs.state`, or a SQL migration that omits it.
- **C2:** a `NamingStrategy`/`NameReplacer` on the `gorm.Config`, or a
  `column:monitor_eps` tag on the field.

## Could not verify

- **The live Postgres column set** — no database access. Whether a deployed volume holds
  `monitor_e_ps`, `monitor_eps` or both is inferred from `AutoMigrate` being additive, not
  observed; the reconciliation above is written to be correct under either.
- **Whether any automatic retry can re-queue a failed monolithic job.** I verified
  `finishJob` writes `failed` (`backend/cmd/worker/main.go:1242-1245`); I did not trace
  every path that could reset a job to `queued`, so "permanent" rests on the finaliser plus
  the absence of any requeue in the artist-delete path. UI retry is explicit and manual
  (`/api/jobs/:id/retry`), consistent with terminal.
- **End-to-end rendering of an owner-less row** — Claim A rests on the query filter and
  the template's absent wording, not on observing a running instance.
- **GORM's derived name was derived, not executed.** I read the naming configuration and
  reasoned from the default strategy; `artist_repoint_test.go:105` corroborates, but I did
  not run GORM here.

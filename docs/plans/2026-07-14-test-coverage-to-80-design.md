# Test Coverage to 80% — Design

> **Last updated:** 2026-07-15 (Phase 4 results — Postgres-enabled final)

**Goal:** Bring every production package from current coverage to ≥80% per-package.

**Architecture:** 1 Linear Initiative → 5 Projects → ~40 Issues, executed in dependency order (low-hanging fruit first, hardest last).

**Tech Stack:** Go 1.25+, `go test` with SQLite `:memory:` + `AutoMigrate`, table-driven tests, mock providers, `net/http/httptest` for HTTP clients.

---

## Current Status (End of Phase 4 — Postgres-enabled Final)

| Package | Before | After | Δ | 80%? |
|---------|--------|-------|---|------|
| **config** | 55.2% | **85.4%** | +30.2% | ✅ |
| **metrics** | 0.0% | **100.0%** | +100% | ✅ |
| **api/templates** | 0.0% | **85.7%** | +85.7% | ✅ |
| **database** | 49.1% | **87.5%** | +38.4% | ✅ (with Postgres) |
| **agent (internal)** | 37.4% | **83.4%** | +46.0% | ✅ |
| **cli** | 3.4% | **73.9%** | +70.5% | ⬆️ +60 tests |
| **api** | 21.6% | **46.7%** | +25.1% | ⬆️ +80 tests |
| **worker** | 15.9% | **41.5%** | +25.6% | ⬆️ +25 tests |
| **services** | 41.5% | **55.0%** | +13.5% | ⬆️ +95 tests |
| **agent (cmd)** | 0.0% | **0.0%** | — | ⬇️ needs refactoring |

**5 packages at 80%+** (config, metrics, templates, database, agent). **~400+ new test functions** added across 4 phases.

### Phase 4 Additions
- **Database Postgres tests:** 6 new tests for `PostgresLockManager.AcquireTryLock`, `ReleaseLock`, `NewLockManager` PG branch, `Migrate()` PG branch. Required Docker Postgres + port mapping.
- **CLI edge cases:** 9 new tests for error paths (no default profile, malformed JSON, duplicate paths, not-found UUIDs).
- **Services acquisition pipeline:** 32 new tests for 6 stage functions (`stageSelectBestResult`, `stageSearchSoulseek`, `stageCheckGonicIndex`, `stageYtdlpFallback`, `Execute`, `stageAlbumBrowse`). Refactored `AcquisitionHandler` to use interfaces for testability.

### Bugs Fixed During Testing
1. **stream.go:** File handle leak — `defer f.Close()` incompatible with Fiber's `SendStream`
2. **CancelJob/RetryJob:** Used `"state"` instead of `"status"` column for JobItem → silent no-op
3. **QualityProfile model:** `gorm:"default:true"` tag caused zero-value bug in CreateProfile
4. **GetJobStats/GetStatsSummary (agent):** PG-only `FILTER (WHERE ...)` → portable `SUM(CASE WHEN ...)` for SQLite compat

### Tests Written (by area)

| Area | Test files | Test functions | Key coverage |
|------|-----------|----------------|--------------|
| config | config_test.go | 20 funcs, 47 subtests | applyOverlay, loadYAMLOverrides, proxy validation, helpers |
| database | 10 test files | ~36 funcs | LiteFSGuard, JSONStringArray, PeerReputation, LockManager, Migrate, Connect, **PostgresLockManager (PG advisory locks), Migrate PG branch** |
| metrics | metrics_test.go | 4 funcs | TrackExternalCall success/error/different services |
| templates | pongo2_engine_test.go | 15 funcs | Render, Load, tryConvertToMap |
| disk_quota_service | services/*_test.go | 20 subtests | FormatBytes, CalculateLibraryUsage, CheckQuotaAlert |
| zombie_recovery | services/*_test.go | 6 tests | 4 cleanup scenarios, config, error paths |
| cover_art | services/*_test.go | 25 tests | CoverArtCacheKey, ParseCoverArtSources |
| import_file | services/*_test.go | 6 subtests | moveFile, failItem with retry/backoff |
| sync_handler | services/*_test.go | 6 tests | isSpotifySourceType |
| job_item_processor | services/*_test.go | 3 tests | ClaimNextItem, RunSafely, NewJobItemProcessor |
| acquisition_pipeline | services/*_test.go | 41 tests | stageLoadItemContext (5), Execute (5), ExecuteItem (1), **stageSelectBestResult (4), stageSearchSoulseek (3), stageCheckGonicIndex (4), stageYtdlpFallback (5), stageAlbumBrowse (5)** |
| cache_service | services/*_test.go | 21 tests | All 7 CRUD functions, 85.2% file coverage |
| metadata_extractor | services/*_test.go | 15+ tests | IsValid, IsAudioFile, SanitizeFilename, getExt, GenerateLibraryPath |
| spotify_graphql | services/*_test.go | 15+ tests | ParseLikedSongs, ParseTrackItems, ParseHomeFeed, ExtractIDFromURI |
| artist_tracking | services/*_test.go | 13 tests | AddMonitoredArtist, GetMonitoredArtists, UpdateArtistStatus, DeleteMonitoredArtist |
| musicbrainz_service | services/*_test.go | 12 tests | SearchArtist, GetRelease, GetReleaseByArtistTitle, GetCoverArt (httptest) |
| discogs_service | services/*_test.go | 10 tests | SearchArtist, GetArtist, GetArtistReleases (httptest) |
| gonic_client | services/*_test.go | 10 tests | Ping, ScanLibrary, TriggerScan, GetAlbumList (httptest) |
| navidrome_client | services/*_test.go | 10 tests | Ping, ScanLibrary, TriggerScan, GetAlbumList, GetNowPlaying (httptest) |
| pages | api/pages_test.go | 14 subtests | 6 page shell handlers + RenderPage |
| partials | api/partials_test.go | 17 tests | RenderJobLogsPartial, RenderJobsPartial |
| playlists | api/playlists_test.go | 32 tests | CRUD + track operations + auth + HTMX |
| stream | api/stream_test.go | 35 subtests | detectAudioContentType (27), StreamTrack (8) |
| subsonic | api/subsonic_test.go | 80 tests | Pure helpers, response helpers, stubs, auth, DB handlers, Search3, GetAlbumList2, GetIndexes, Playlists CRUD |
| agent handlers | agent/handlers_test.go | 28 test cases | SyncWatchlist, ListDuplicates, GetJobStats, GetStatsSummary + existing |
| worker | cmd/worker/worker_test.go | 25 tests | UpdateHeartbeats, TriggerLibraryScan, CheckQuotaAlerts, FinishJob, TriggerWatchlistSyncs, ClaimAndProcess, ProcessActiveJobsRoundRobin, SchedulerLoop, **RunMonolithicJob** |
| cli | cmd/cli/main_test.go | 53 tests | formatFileSize, printJSON, handleError, all command runners (watchlist, library, profile, stats) with mock server, **edge cases (no default profile, malformed JSON, duplicate paths, not-found UUIDs)** |

---

## Excluded from Target

| Package | Reason |
|---------|--------|
| `cmd/test_sqlite` | Smoke test binary, not production code |
| `internal/interfaces` | Compile-time contracts — no behavior to test |
| `internal/testutil` | Test infrastructure — coverage counted where used |

---

## Project Structure

### ✅ Project 1: Config & Database Coverage (Quick Wins)
**Packages:** `internal/config` (55.2% → **85.4%** ✅), `internal/database` (49.1% → **87.5%** ✅ with Postgres)
**Status:** Both done. Database reached 80%+ after adding Postgres-specific tests (advisory locks, Migrate PG branch). Requires `DATABASE_URL` env var pointing to live Postgres for full coverage; tests skip gracefully without it.

### ✅ Project 2: Metrics & Templates Coverage (Quick Wins)
**Packages:** `internal/metrics` (0% → **100%** ✅), `internal/api/templates` (0% → **85.7%** ✅)
**Status:** Both done.

### ✅ Project 3: Services Coverage (Medium)
**Package:** `internal/services` (41.5% → **55.0%**)
**Status:** Significant progress. 95+ new tests added across 4 phases.

**Tested:**
- disk_quota_service.go ✅
- zombie_recovery.go ✅
- cover_art.go ✅
- import_file.go (moveFile, failItem) ✅
- sync_handler.go (isSpotifySourceType) ✅
- job_item_processor.go ✅
- acquisition_pipeline.go (stageLoadItemContext, Execute, ExecuteItem) ✅
- cache_service.go (21 tests, 85.2% file coverage) ✅
- metadata_extractor.go (pure helpers) ✅
- spotify_graphql.go (parsers) ✅
- artist_tracking_service.go (13 CRUD tests) ✅
- musicbrainz_service.go (12 tests, httptest) ✅
- discogs_service.go (10 tests, httptest) ✅
- gonic_client.go (10 tests, httptest) ✅
- navidrome_client.go (10 tests, httptest) ✅

**Remaining gaps:**
- spotify_spdc.go, spotify_provider.go — OAuth/SPDC flows
- notification_service.go — multi-channel dispatch
- slskd_service.go — Soulseek client (15+ methods)
- acoustid_service.go — fingerprinting
- watchlist_service.go — track fetching
- sync_handler.go Execute() — full sync flow
- acquisition_pipeline.go — deeper pipeline stages
- import_file.go importFile() — full flow

### ✅ Project 4: API Layer Coverage (Medium-Hard)
**Package:** `internal/api` (21.6% → **46.7%**)
**Status:** Major progress. 80+ new tests for Subsonic API.

**Tested:**
- pages.go — 6 page shell handlers + RenderPage ✅
- partials.go — RenderJobLogsPartial, RenderJobsPartial ✅
- playlists.go — 10 CRUD handlers + auth + HTMX ✅
- stream.go — detectAudioContentType + StreamTrack ✅
- subsonic.go — 80 tests: pure helpers, response helpers, stubs, auth middleware, DB handlers, Search3, GetAlbumList2, GetIndexes, Playlists CRUD ✅

**Remaining gaps:**
- subsonic.go — GetAlbumList (v1), Stream, GetCoverArt, GetIndexes (deeper), file I/O handlers
- spotify_auth.go — OAuth flow
- websocket_manager.go — WebSocket events
- watchlist_preview.go — HTTP handler
- admin handlers, LiteFS middleware, middleware.go remaining paths

### ✅ Project 5: CLI, Agent & Worker Coverage (Hardest)
**Packages:** `cmd/agent` (0%), `cmd/cli` (3.4% → **73.9%**), `cmd/worker` (15.9% → **41.5%**), `internal/agent` (37.4% → **83.4%** ✅)
**Status:** Agent done. CLI and Worker made major progress. cmd/agent still needs refactoring.

**Tested:**
- internal/agent — all 18+ handler functions tested ✅
- cmd/worker — 25 tests: heartbeat, triggerLibraryScan, quota alerts, finishJob, triggerWatchlistSyncs, claimAndProcess, processActiveJobsRoundRobin, schedulerLoop, runMonolithicJob ✅
- cmd/cli — 53 tests: formatFileSize, printJSON, handleError, all command runners with mock HTTP server, edge cases (no default profile, malformed JSON, duplicate paths, not-found UUIDs) ✅

**Remaining gaps:**
- cmd/worker — deeper scheduler internals, complex job processing edge cases
- cmd/cli — remaining edge cases in command parsing
- cmd/agent — all handlers are closures in main(), needs refactoring to extract constructors

---

## Test Pattern

All tests follow the established pattern:
```go
func TestXxx(t *testing.T) {
    db, _ := database.Connect(&config.Config{DatabaseURL: ":memory:"})
    database.Migrate(db)
    // ... test logic
}
```

Service tests inject nil for unused dependencies:
```go
handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
```

API tests use the `setupAPITestDB` + `newTestApp` pattern with user middleware injection.

**Postgres-specific tests** (database package) use env-var gating:
```go
dbURL := os.Getenv("DATABASE_URL")
if dbURL == "" {
    t.Skip("DATABASE_URL not set")
}
```
Run with: `DATABASE_URL=postgresql://musicops:password@localhost:5432/musicops go test ./internal/database`

---

## Success Criteria

- `go test ./... -cover` shows ≥80% for every production package (with Postgres for database)
- Tests skip gracefully when Postgres unavailable (database PG-specific tests)
- No regression in existing test suite
- Bugs discovered during testing are fixed (not just worked around in tests)

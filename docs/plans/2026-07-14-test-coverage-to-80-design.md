# Test Coverage to 80% — Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring every production package from current coverage to ≥80% per-package.

**Architecture:** 1 Linear Initiative → 5 Projects → ~40 Issues, executed in dependency order (low-hanging fruit first, hardest last).

**Tech Stack:** Go 1.25+, `go test` with SQLite `:memory:` + `AutoMigrate`, table-driven tests, mock providers.

---

## Excluded from Target

| Package | Reason |
|---------|--------|
| `cmd/test_sqlite` | Smoke test binary, not production code |
| `internal/interfaces` | Compile-time contracts — no behavior to test |
| `internal/testutil` | Test infrastructure — coverage counted where used |

## Project Structure

### Project 1: Config & Database Coverage (Quick Wins)
**Packages:** `internal/config` (55.2% → 80%), `internal/database` (49.1% → 80%)
**Est. Issues:** 6
**Why first:** Both use the established SQLite `:memory:` pattern. Config needs ~10 tests for untested helpers. Database needs ~20 tests for untested model helpers and LiteFS guard.

### Project 2: Metrics & Templates Coverage (Quick Wins)
**Packages:** `internal/metrics` (0% → 80%), `internal/api/templates` (0% → 80%)
**Est. Issues:** 4
**Why second:** Tiny packages. Metrics has 1 function (`TrackExternalCall`). Templates has 1 file needing temp-dir tests. Both can be done in a single session each.

### Project 3: Services Coverage (Medium)
**Packages:** `internal/services` (41.5% → 80%)
**Est. Issues:** 8
**Why third:** Already has 40 test files and established mock patterns. Needs tests for 7 untested areas: `cover_art.go`, `import_file.go`, `disk_quota_service.go`, `zombie_recovery.go`, `acquisition_pipeline.go`, `sync_handler.go`, `job_item_processor.go`.

### Project 4: API Layer Coverage (Medium-Hard)
**Packages:** `internal/api` (21.6% → 80%)
**Est. Issues:** 10
**Why fourth:** 21 source files, 9 completely untested. Needs tests for admin handler, Subsonic handler, playlists, streaming, Spotify auth, WebSocket manager, HTMX partials error paths, LiteFS middleware.

### Project 5: CLI, Agent & Worker Coverage (Hardest)
**Packages:** `cmd/agent` (0% → 80%), `cmd/cli` (3.4% → 80%), `cmd/worker` (15.9% → 80%), `internal/agent` (37.4% → 80%)
**Est. Issues:** 12
**Why last:** Requires extracting testable functions from monolithic `main.go` files. `cmd/worker` is the hardest — `WorkerOrchestrator` has 20+ injected services and private goroutine-heavy methods. `cmd/cli` needs DB wiring extraction from `PersistentPreRun`.

## Test Pattern

All new tests follow the established pattern:
```go
func TestXxx(t *testing.T) {
    db := database.Connect("file::memory:?cache=shared")
    database.Migrate(db)
    // ... test logic
}
```

No new test infrastructure needed. External services mocked via existing `testutil.MockProvider` and `httptest.NewRecorder`.

## Success Criteria

- `go test ./... -cover` shows ≥80% for every production package
- No new integration dependencies (all tests pass without Docker/Postgres/slskd)
- No regression in existing test suite

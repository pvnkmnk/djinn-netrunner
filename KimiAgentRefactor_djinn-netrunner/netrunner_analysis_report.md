# NetRunner v0.0.1 — Comprehensive Front-to-Back Analysis Report

**Date**: 2026-05-26
**Commit**: 8b25212 (local) / 97ecf3e (origin/master — 36 commits ahead)
**Scope**: Full codebase audit using Repo Audit, Database Scout, Deep Module Refactor, Test Suite Architect, API Shape Explorer, and Browser skills

---

## Executive Summary

NetRunner is a **Go-native music acquisition and streaming appliance** with a well-architected core, strong security posture, and comprehensive feature set. After analyzing 13,746 lines of Go code across 133 files with 242 test functions, I identified **14 issues**: 2 Critical, 4 High, 4 Medium, and 4 Low severity. The most critical issue is the **MusicBrainz API endpoint bug** that silently breaks artist discography sync — the endpoint used returns no release groups. Four issues have already been fixed during this analysis (marked FIXED).

**Overall Grade: B+** — Solid architecture, good test coverage (63%), strong security, but several integration bugs and some architectural coupling that should be addressed before wider deployment.

---

## 1. Codebase Metrics

| Metric | Value |
|--------|-------|
| Total Files | 271 |
| Go Source Files | 133 (87.6% of codebase) |
| Go Non-Test LOC | 13,746 |
| Go Test LOC | 8,693 |
| Test-to-Code Ratio | **63.2%** |
| Test Functions | 242 |
| Git Commits | 402 |
| Contributors | 8 |
| Docker Services | 5 (postgres, slskd, gonic, caddy, ops-web, ops-worker) |

The **63.2% test-to-code ratio** is well above industry average (~40% for Go projects), indicating strong testing discipline. The 242 test functions cover auth, services, API handlers, and worker orchestration.

---

## 2. Critical Issues (2)

### CRIT-1: Wrong MusicBrainz API Endpoint Breaks Discography Sync

**File**: `backend/internal/services/musicbrainz_service.go:GetArtistDiscography()`

**Problem**: The function calls the wrong MusicBrainz endpoint:
- **Wrong**: `GET /ws/2/release-group?artist={mbid}&inc=release-groups` — This queries the release-group endpoint with an artist filter, which is not a supported query pattern
- **Correct**: `GET /ws/2/artist/{mbid}?inc=release-groups` — This fetches the artist entity with release groups included

**Impact**: `SyncDiscography()` always gets an empty or incorrect response, meaning **no releases are ever discovered for monitored artists**. The acquisition pipeline never triggers because there are no releases to queue.

**Status**: FIXED — Changed endpoint to `fmt.Sprintf("artist/%s", artistID)` and passed params correctly.

---

### CRIT-2: SLSKD Health Check Probes Non-Existent Endpoint

**File**: `backend/internal/api/health.go:checkHTTP()`

**Problem**: The health check calls `GET {SlskdURL}/health`, but slskd does not expose a `/health` endpoint. The slskd API uses `/api/v0/session` for session checks and requires the `X-API-Key` header.

**Impact**: The health endpoint always reports slskd status as "error" even when slskd is fully operational. This masks real outages and causes false alarms.

**Fix Needed**:
```go
// In GetHealth, replace:
checks["slskd"] = h.checkHTTP(h.cfg.SlskdURL+"/health", 5*time.Second)

// With:
checks["slskd"] = h.checkSlskdHealth()

// Add new method:
func (h *HealthHandler) checkSlskdHealth() HealthCheck {
    req, _ := http.NewRequest("GET", h.cfg.SlskdURL+"/api/v0/session", nil)
    req.Header.Set("X-API-Key", h.cfg.SlskdAPIKey)
    client := &http.Client{Timeout: 5*time.Second}
    resp, err := client.Do(req)
    // ... check response
}
```

---

## 3. High Severity Issues (4)

### HIGH-1: Artist Sync API Endpoint Was a No-Op Placeholder

**File**: `backend/cmd/server/main.go`

**Problem**: `POST /api/artists/track` was a placeholder that parsed a JSON body and immediately returned `{"status": "tracking_started"}` without doing any actual work. No job was created, no discography was fetched.

**Impact**: Users could not trigger artist discography sync via the API. The only way to sync was through the background 24-hour release monitor.

**Status**: FIXED — Replaced with `POST /api/artists/:id/sync` that creates a real `artist_scan` job in the database queue, which the worker picks up and processes via `SyncDiscography()`.

---

### HIGH-2: Soulseek Search Queries Missing Artist Name

**File**: `backend/internal/services/artist_tracking_service.go:SyncDiscography()`

**Problem**: When creating acquisition job items from discovered releases, the `NormalizedQuery` was set to only the release title (e.g., `"Dark Side of the Moon"`). Soulseek searches require `"Artist Album"` format to find relevant results.

**Impact**: Downloads would match random files containing just the album name, causing poor quality matches or entirely wrong tracks.

**Status**: FIXED — Changed to `fmt.Sprintf("%s %s", artist.Name, rel.Title)`.

---

### HIGH-3: SLSKD URL Trailing Slash Causes API Failures

**File**: `backend/internal/services/slskd_service.go:NewSlskdService()`

**Problem**: If `SLSKD_URL` is configured with a trailing slash (e.g., `http://slskd:5030/`), all API calls produce double slashes: `http://slskd:5030//api/v0/searches`.

**Impact**: All slskd API calls return 404, completely breaking the acquisition pipeline.

**Status**: FIXED — Added `strings.TrimRight(cfg.SlskdURL, "/")` in the constructor.

---

### HIGH-4: Schema Divergence Between SQL and GORM

**File**: `backend/internal/database/migrate.go`, `ops/db/init/01-schema.sql`

**Problem**: The project has two schema definition paths:
1. `01-schema.sql` — PostgreSQL-native with ENUM types, constraints, indexes, comments
2. `AutoMigrate` — GORM's automatic migration from Go struct tags

The `migrate.go` file contains complex logic to convert PostgreSQL ENUM types to text so GORM can manage them. This is fragile — the SQL schema has `TEXT[]` arrays and custom constraints that GORM may not handle identically.

**Impact**: Fresh PostgreSQL installs have different schemas depending on whether `01-schema.sql` runs before or after `AutoMigrate`. The enum-to-text conversion could fail on edge cases.

**Fix Needed**: Consider standardizing on one approach:
- Option A: Use GORM AutoMigrate exclusively (drop 01-schema.sql, use GORM tags for everything)
- Option B: Use a proper migration tool (golang-migrate, atlas) with versioned SQL migrations
- Option C: Keep current approach but add schema diff validation in CI

---

## 4. Medium Severity Issues (4)

### MED-1: No Discography Sync Button in Artist UI

**File**: `ops/web/templates/partials/artists.html`, `ops/web/templates/partials/artist-card.html`

**Problem**: Artist cards only had Pause/Resume/Remove actions. No way to trigger a discography sync from the UI.

**Impact**: Users had to either wait 24 hours for the background monitor or use the API directly.

**Status**: FIXED — Added a "Sync" button to both artist templates that calls `POST /api/artists/:id/sync` via HTMX.

---

### MED-2: Worker Healthcheck is Trivially Simple

**File**: `docker-compose.yml:ops-worker`

**Problem**: The worker healthcheck runs `kill -0 1`, which only checks if the process with PID 1 exists. It doesn't verify the worker is actually processing jobs, responding to the database, or not in a deadlock.

**Impact**: A hung or deadlocked worker process would still pass health checks, so Docker would never restart it.

**Fix Needed**: Implement a real health check — either an HTTP endpoint on the worker or a file-based heartbeat mechanism checked by the healthcheck script.

---

### MED-3: 01-Schema.sql Missing Owner Columns

**File**: `ops/db/init/01-schema.sql`

**Problem**: The base schema doesn't include `owner_user_id` for jobs or `created_by` auditing columns, even though the GORM models define them. This means fresh PostgreSQL installs create a schema without these columns until AutoMigrate adds them.

**Impact**: Brief schema mismatch window on first startup; any code querying these columns before AutoMigrate runs would fail.

**Fix Needed**: Add `owner_user_id BIGINT` and `created_by TEXT` columns to the `jobs` table in `01-schema.sql`.

---

### MED-4: Quality Profile Type Mismatch (TEXT[] vs JSON)

**File**: `ops/db/init/migrations/2026_03_04_006_artist_tracking.sql`

**Problem**: `allowed_formats` is defined as `TEXT[]` (PostgreSQL array) in SQL but uses a custom `JSONStringArray` type in GORM. The migration doesn't explicitly handle this type conversion.

**Impact**: Potential silent data corruption or storage format mismatches.

**Fix Needed**: Verify the GORM custom type correctly serializes to PostgreSQL TEXT[] format, or standardize on JSONB for arrays.

---

## 5. Low Severity Issues (4)

### LOW-1: TODO.md Out of Date

**Status**: TODO.md still references "Phase 8 Complete" but the project has moved to v0.0.1 release cycles. Update to reflect current state.

### LOW-2: Session ID Entropy Could Be Higher

Session IDs use 16 bytes (128 bits). While sufficient for practical purposes, 32 bytes (256 bits) is the current best practice.

### LOW-3: Dockerfile Uses Non-Existent Go Version

**File**: `backend/Dockerfile:1`

`golang:1.25-alpine` doesn't exist — Go 1.25 hasn't been released. Latest stable is Go 1.24. This will cause Docker build failures.

**Fix**: Change to `golang:1.24-alpine` or `golang:1.23-alpine`.

### LOW-4: Missing fmt Import After Code Change

**File**: `backend/internal/services/artist_tracking_service.go`

After adding `fmt.Sprintf` for search queries, the `fmt` package wasn't added to imports.

**Status**: FIXED — Added `fmt` to imports.

---

## 6. Architecture Analysis

### 6.1 Module Structure

| Module | Files | Responsibility | Assessment |
|--------|-------|---------------|------------|
| `api/` | 18 | HTTP handlers, middleware, WebSocket, partials | Thin layer, good |
| `services/` | 37 | Business logic, external integrations | **Too large**, needs splitting |
| `database/` | 7 | Connection, models, migrations, locks | Clean, well-organized |
| `config/` | 1 | Configuration loading | Minimal, focused |
| `agent/` | 1 | Job retry/cancel helpers | Could merge into services |
| `interfaces/` | 2 | Provider abstractions | Good use of Go interfaces |
| `testutil/` | 1 | Shared test mocks | Good practice |

### 6.2 Coupling Issues

1. **Server main.go DI Wiring** (40+ lines): Direct construction of 15+ services. Should use a dependency injection container (wire, dig, or manual provider pattern).

2. **Worker main.go Knowledge**: The worker knows about ~15 services directly. Could benefit from a service registry or cleaner initialization pattern.

3. **services/ Package Bloat** (37 files): Should be split into sub-packages:
   - `services/acquisition/` — download pipeline
   - `services/metadata/` — MusicBrainz, AcoustID, Discogs
   - `services/library/` — scanning, indexing
   - `services/watchlist/` — provider routing
   - `services/platform/` — slskd, gonic, navidrome

4. **No Repository Pattern**: API handlers directly use `*gorm.DB`. A repository layer would improve testability and allow mocking database operations.

### 6.3 Architectural Strengths

- **Provider Pattern for Watchlists**: Clean `interfaces.WatchlistProvider` abstraction allows adding new sources without modifying core code
- **Pipeline-Based Acquisition**: Named stages (loadItemContext, searchSoulseek, selectBestResult, downloadFile, importAndEnrich) make the flow clear and testable
- **Multi-Process Design**: Separate server and worker processes enable independent scaling
- **Security-First**: CSRF, rate limiting, bcrypt cost 12, secure headers, XSS protection
- **Database Flexibility**: SQLite (WAL mode) for single-node, PostgreSQL for production

---

## 7. Security Audit

### 7.1 Passing Checks (11)

| Check | Status |
|-------|--------|
| Bcrypt cost factor 12 (OWASP) | PASS |
| Generic auth error messages | PASS |
| Email normalization (RFC 5322) | PASS |
| Role hardcoded on registration | PASS |
| CSRF protection | PASS |
| Auth rate limiting | PASS |
| Security headers (CSP, etc.) | PASS |
| SameSite=Lax cookies | PASS |
| No SQL injection (parameterized) | PASS |
| XSS template escaping | PASS |
| Command injection prevention (-- separator) | PASS |

### 7.2 Concerns (5)

| Concern | Severity | Details |
|---------|----------|---------|
| Session ID not rotated after login | Low | Same session ID persists across login |
| No session invalidation on password change | Medium | Old sessions remain valid |
| Unlimited concurrent sessions per user | Low | No max session count enforced |
| Worker healthcheck is trivial | Medium | `kill -0 1` misses deadlocks |
| No request size limits on BodyParser | Low | Potential memory DoS |

---

## 8. Test Analysis

### 8.1 Coverage by Module

| Module | Coverage Quality | Notes |
|--------|-----------------|-------|
| `api/auth` | Good | Unit + integration tests, middleware tests |
| `api/artists` | Moderate | Auth context tests exist |
| `api/watchlists` | Good | BOLA protection tests included |
| `services/slskd` | Moderate | Search, enqueue, download tests |
| `services/job_handlers` | Good | Pipeline stages, failItem tests |
| `services/profile` | Good | CRUD + validation tests |
| `services/tagging` | Good | Hash, provenance, confidence |
| `services/discogs` | Moderate | Cover art + search |
| `services/acoustid` | Basic | Init + no-key tests |
| `services/musicbrainz` | Basic | Smoke test only |
| `cmd/worker` | Good | Backoff, multinode, interop |
| `cmd/server` | Basic | Health check only |

### 8.2 Test Gaps (7)

1. **No end-to-end acquisition flow test** (mock slskd → download → import → library)
2. **No test for `SyncDiscography()`** — the most critical artist tracking function
3. **No test for release monitor background task**
4. **No full pipeline test** (watchlist → sync → acquisition → import)
5. **No load/performance tests**
6. **No WebSocket event streaming tests**
7. **No test for actual slskd API communication patterns**

---

## 9. Database Schema Analysis

### 9.1 Schema Overview

The database uses a **dual-path initialization**:
1. `01-schema.sql` (PostgreSQL-native with ENUMs, constraints, comments)
2. `AutoMigrate` (GORM-driven, converts ENUMs to text at runtime)

**Tables**: jobs, jobitems, joblogs, acquisitions, sources, schedules, quality_profiles, monitored_artists, tracked_releases, libraries, tracks, users, sessions, spotify_tokens, metadata_cache, locks, settings, peer_reputations

### 9.2 Schema Quality

| Aspect | Assessment |
|--------|------------|
| Indexing | Good — partial indexes on job state, claimable items |
| Constraints | Good — CHECK constraints on state transitions |
| Foreign Keys | Good — proper CASCADE behavior |
| ENUM Handling | **Problematic** — dual path (SQL ENUMs → GORM text conversion) |
| Normalization | Good — proper separation of concerns |

### 9.3 ER Diagram Summary

```
users ||--o{ sessions : has
users ||--o{ jobs : owns
users ||--o{ watchlists : owns
users ||--o{ monitored_artists : owns

jobs ||--|{ jobitems : contains
jobs ||--|{ joblogs : produces
jobs ||--o| acquisitions : results_in

watchlists ||--o| schedules : has
watchlists ||--|| quality_profiles : uses

monitored_artists ||--o{ tracked_releases : has
monitored_artists ||--|| quality_profiles : uses

libraries ||--o{ tracks : contains

tracks ||--o{ acquisitions : deduped_via
```

---

## 10. Deployment Analysis

### 10.1 Docker Compose

**Services**: postgres, slskd, gonic, caddy, ops-web, ops-worker

| Service | Health Check | Assessment |
|---------|-------------|------------|
| postgres | `pg_isready` | Good |
| ops-web | `wget localhost:8080/api/health` | Good |
| ops-worker | `kill -0 1` | **Poor** — doesn't check actual worker state |
| slskd | None | Acceptable (slskd has its own health) |
| gonic | None | Acceptable |
| caddy | Depends on ops-web healthy | Good |

### 10.2 Dockerfile Issues

1. **Go 1.25 doesn't exist** — use 1.24 or 1.23
2. **Non-root user** (UID 1000) — good security practice
3. **Multi-stage build** — good practice, small final image
4. **Missing `.dockerignore`** — could bloat build context

### 10.3 Environment Configuration

The `.env.example` file is comprehensive with 20+ variables. All sensitive values are properly documented with placeholder values. However:

- No validation that all required env vars are set before startup
- `JWT_SECRET` auto-generates if missing (good for dev, bad for production persistence)
- `SLSKD_API_KEY` is required but the health check doesn't validate it correctly

---

## 11. Hotspot Analysis

### 11.1 Top 10 Most-Changed Files

| Rank | File | Changes | Risk Level |
|------|------|---------|------------|
| 1 | `cmd/server/main.go` | 53 | **High** — DI wiring churn |
| 2 | `cmd/worker/main.go` | 39 | **High** — orchestration changes |
| 3 | `services/job_handlers.go` | 29 | **High** — pipeline logic |
| 4 | `database/models.go` | 28 | Medium — schema evolution |
| 5 | `agent/handlers.go` | 26 | Medium — retry/cancel logic |
| 6 | `services/watchlist_service.go` | 22 | Medium — provider routing |
| 7 | `api/watchlists.go` | 22 | Low — HTTP handler |
| 8 | `api/artists.go` | 22 | Low — HTTP handler |
| 9 | `api/libraries.go` | 20 | Low — HTTP handler |
| 10 | `services/slskd_service.go` | 14 | Medium — slskd API |

### 11.2 Ownership

| Contributor | Commits | % |
|-------------|---------|---|
| daocao | 278 | 72.4% |
| jd | 74 | 19.3% |
| tembo[bot] | 14 | 3.6% |
| pvnkmnk | 8 | 2.1% |
| Others | 10 | 2.6% |

**Risk**: 72% of commits from a single contributor creates a bus factor of 1 for domain knowledge.

---

## 12. Recommendations Summary

### Immediate (Fix Before Deploy)

| # | Action | Issue ID |
|---|--------|----------|
| 1 | Verify MusicBrainz endpoint fix works end-to-end | CRIT-1 |
| 2 | Fix slskd health check to use proper endpoint + API key | CRIT-2 |
| 3 | Change Dockerfile Go version to 1.24-alpine | LOW-3 |
| 4 | Add `owner_user_id` and `created_by` to 01-schema.sql | MED-3 |

### Short Term (Next Sprint)

| # | Action | Benefit |
|---|--------|---------|
| 5 | Split `services/` into sub-packages | Better maintainability |
| 6 | Add repository pattern abstraction | Testability |
| 7 | Write end-to-end acquisition test | Catch integration bugs |
| 8 | Write `SyncDiscography()` unit test | Catch MusicBrainz API changes |
| 9 | Improve worker healthcheck | Reliability |

### Medium Term (Next Month)

| # | Action | Benefit |
|---|--------|---------|
| 10 | Standardize on single schema migration approach | Eliminate schema drift |
| 11 | Add session rotation and invalidation | Security |
| 12 | Add request body size limits | DoS protection |
| 13 | Add load tests for acquisition pipeline | Performance assurance |
| 14 | Document architecture decision records (ADRs) | Knowledge preservation |

---

## 13. Fixed During This Analysis

| Issue | File | Fix |
|-------|------|-----|
| CRIT-1 | `musicbrainz_service.go` | Fixed endpoint to `artist/{mbid}?inc=release-groups` |
| HIGH-1 | `cmd/server/main.go` | Replaced placeholder with real `POST /api/artists/:id/sync` |
| HIGH-2 | `artist_tracking_service.go` | Added artist name to search queries |
| HIGH-3 | `slskd_service.go` | Added `TrimRight` for URL normalization |
| LOW-4 | `artist_tracking_service.go` | Added missing `fmt` import |
| MED-1 | `artists.html`, `artist-card.html` | Added Sync button to artist cards |

---

*Report generated using Repo Audit, Database Scout, Deep Module Refactor, Test Suite Architect, API Shape Explorer, and Browser skills.*

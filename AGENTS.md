# Agents Guide - NetRunner

> Last updated: 2026-07-14

## Codemap

Before working on any task, read `codemap.md` in the project root to understand:
- **Project architecture and entry points** — System entry points (`server`, `worker`, `agent`, `cli`)
- **Directory responsibilities** — Detailed codemaps per directory for deep work
- **Data flow and integration points** — How watchlists, acquisition, metadata, and library management connect
- **Architecture constraints** — Technology choices, invariants, and design patterns

For deep work on a specific folder, also read that folder's `codemap.md` (e.g., `backend/internal/services/codemap.md` for the service layer).

## Overview
NetRunner is a Go-based music acquisition and library-operations platform. It ingests tracks from watchlist sources (Spotify, Last.fm, ListenBrainz, RSS, local files), orchestrates acquisition jobs through slskd (Soulseek daemon), enriches metadata, and maintains local music libraries with a Fiber + HTMX UI.

Agents working in this repository should assume read-write autonomy for local code and docs updates, test execution, and non-destructive tooling changes. Runtime/deployment actions (docker compose changes, production environment changes, credential rotation) should be treated as operator-reviewed actions.

Current authentication is session-cookie based (`session_id`) with role checks (`user`, `admin`) at handler/service boundaries. This differs from the older JWT-first description and should be considered the source of truth for API behavior.

## Repository Map

| Directory | Type | Purpose | Agent-relevant? |
|---|---|---|---|
| `backend/cmd/` | Go executables | Entry points for `server`, `worker`, `cli`, `agent`, `test_sqlite` — see [codemap](backend/cmd/codemap.md) for per-binary details | Yes |
| `backend/internal/api/` | HTTP layer | Fiber handlers, route logic, auth/session middleware, HTMX/WebSocket endpoints | Yes |
| `backend/internal/api/templates/` | View engine | Pongo2 Jinja2-compatible Fiber ViewEngine adapter | Yes |
| `backend/internal/services/` | Service layer | Acquisition orchestration, providers, API clients, metadata/tagging, notifications | Yes |
| `backend/internal/database/` | Data layer | GORM models, connection, migrations, lock managers, helpers | Yes |
| `backend/internal/agent/` | Agent facade | Transport-agnostic functions used by MCP server + CLI | Yes |
| `backend/internal/config/` | Config | Env loading and defaults, required/optional config validation | Yes |
| `backend/internal/interfaces/` | Abstractions | `WatchlistProvider` and `SpotifyClientProvider` contracts | Yes |
| `backend/internal/integration/` | Integration tests | Dockerized slskd/integration harness and test scenarios | Yes |
| `backend/internal/testutil/` | Test utilities | Shared test doubles and mock providers for unit testing | Yes |
| `ops/` | Infra config | Docker Compose, Caddy reverse proxy, DB init/migrations, web assets | Yes |
| `ops/caddy/` | Reverse proxy | Caddy config for production HTTPS | Yes |
| `ops/db/` | Database | PostgreSQL init scripts and SQL migrations | Yes |
| `ops/web/` | Web assets | Static files (CSS, JS) + Pongo2 HTML templates | Yes |
| `ops/web/static/js/` | Client JS | Minimal vanilla JS for modals, console, WebSocket | Yes |
| `ops/web/templates/` | HTML templates | Layouts, pages, HTMX partials for server-side rendering | Yes |
| `.github/workflows/` | CI/CD | Go CI (`go vet`, `go test`, coverage artifact), Docker build/push, E2E tests (Playwright), PR reviews (PRGuard, PR-Sentry) | Yes |
| `scripts/` | Ops scripts | Integration test (`integration-tests.sh`), smoke test (`smoke-test.sh`), validation (`validate.sh`, `validate.ps1`) | Yes |
| `e2e/` | Playwright E2E tests | Browser-based E2E tests against Docker Compose stack | Yes |
| `docs/` | Documentation | Architecture/plans/runbooks | Yes |
| `conductor/` | (removed) | Legacy/archived docs area — content captured in Linear | No |
| `examples/` | Examples | Sample content and helper artifacts | Sometimes |

## Quickstart for Agents
1. Clone and enter repo:
   - `git clone <repo-url>`
   - `cd netrunner`
2. Copy env template:
   - `cp .env.example .env`
3. Populate required env vars in `.env`:
   - `DATABASE_URL`, `JWT_SECRET`, `SLSKD_API_KEY`
   - For Docker compose: `POSTGRES_PASSWORD`, `SLSKD_USERNAME`, `SLSKD_PASSWORD`
4. Install dependencies:
   - `cd backend && go mod download`
5. Run baseline validation:
   - `go vet ./...`
   - `go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent`
6. Start dev server:
   - Local binary mode: `go run ./cmd/server`
   - Full stack: `docker compose up -d`

## Environment Variables

See `.env.example` for the full list. Below are the non-obvious or conditionally required ones.

| VAR_NAME | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | Yes | none | Primary DB connection string (`postgres://...` or SQLite `.db` path) |
| `SLSKD_API_KEY` | Yes (for acquisition) | empty | slskd API auth |
| `JWT_SECRET` | Recommended | auto-generated | Session/auth crypto secret; set explicitly for stable restarts |
| `MUSIC_LIBRARY` | No | `./music_library` | Library root path |
| `GONIC_USER` / `GONIC_PASS` | Conditional | empty | Gonic/Subsonic auth |
| `POSTGRES_PASSWORD` | Docker-only | (required) | Postgres password for compose stack |
| `SLSKD_USERNAME` / `SLSKD_PASSWORD` | Docker-only | none | slskd Soulseek credentials |
| `SKIP_INTEGRATION_TESTS` | Test-only | empty | Skip integration tests when `true` |
| `SKIP_NETWORK_TESTS` | Test-only | empty | Skip network-dependent tests |

**Integration-only vars** (for `./scripts/integration-tests.sh`):
- `INTEGRATION_DATABASE_URL`, `INTEGRATION_SLSKD_URL`, `INTEGRATION_SLSKD_API_KEY`
- `SLSKD_TEST_USERNAME`, `SLSKD_TEST_PASSWORD`, `SLSKD_TEST_API_KEY`
- `INTEGRATION_SLSKD_USERNAME`, `INTEGRATION_SLSKD_PASSWORD`
- `INTEGRATION_DB_PASSWORD`

## Available Commands

| Command | Purpose | When to use |
|---|---|---|
| `cd backend && go mod download` | Fetch module dependencies | Fresh clone or dependency changes |
| `cd backend && go vet ./...` | Static correctness checks | Before PR/merge |
| `cd backend && go test ./...` | Full unit/integration (non-tagged) suite | Baseline validation |
| `cd backend && go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent` | Core passing suite in current workspace | Fast confidence check |
| `cd backend && go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent` | Build all primary binaries | Release prep and smoke checks |
| `cd backend && go run ./cmd/server` | Start HTTP server (auto-runs migrations) | Local API/UI development |
| `cd backend && go run ./cmd/worker` | Start worker orchestrator | Local job-processing tests |
| `cd backend && go run ./cmd/agent` | Start MCP server over stdio | Agent tool integration |
| `cd backend && go run ./cmd/cli --help` | Inspect CLI surface | Operational scripting |
| `docker compose up -d` | Bring up full stack | End-to-end local environment |
| `docker compose logs -f netrunner` | Follow app logs | Runtime debugging |
| `docker compose logs -f netrunner-slskd` | Follow slskd logs | Acquisition/connectivity debugging |
| `./scripts/integration-tests.sh test` | Run integration-tagged tests with dockerized deps | Integration scenarios |
| `go test ./backend/internal/integration/... -tags=integration -v` | Direct integration test invocation | CI/debug for integration package |
| `./scripts/smoke-test.sh` | Deploy Docker stack + health/auth/CRUD checks | End-to-end smoke test |
| `./scripts/validate.sh` or `validate.ps1` | Pre-commit validation checks | Before PR/merge |
| `govulncheck ./...` | Vulnerability scan for reachable issues in code + deps | Security/dependency maintenance |
| `cd e2e && npx playwright test` | Run Playwright E2E tests against Docker stack | E2E UI testing |
| `cd e2e && npx playwright test tests/auth.spec.ts` | Run specific E2E test spec | Targeted E2E testing |
| `docker compose -f docker-compose.yml -f docker-compose.e2e.yml up -d` | Start stack with E2E port mapping | E2E test development |

## E2E Testing (Playwright)

E2E tests use Playwright against the full Docker Compose stack (Postgres, slskd, gonic, caddy, web, worker).

**Key files:**
- `e2e/playwright.config.ts` — Playwright config with Docker webServer
- `e2e/fixtures/auth.fixture.ts` — Auth fixtures (`authenticatedPage`, `adminPage`)
- `e2e/setup-test-db.sh` — Postgres test database setup
- `docker-compose.e2e.yml` — Port 8080 exposure + higher rate limit for tests

**Running tests:**
```bash
cd e2e && npx playwright test                    # All tests
cd e2e && npx playwright test tests/auth.spec.ts  # Specific spec
```

**How it works:**
1. `webServer` runs `docker compose -f docker-compose.yml -f docker-compose.e2e.yml up -d --build`
2. `setup-test-db.sh` waits for Postgres, creates fresh `musicops_test` database
3. Tests run against `http://localhost:8080`
4. `globalTeardown` runs `docker compose down -v --remove-orphans` (only in CI — skipped locally for fast iteration)

**Docker reuse (key iteration speedup):**
- `playwright.config.ts` sets `reuseExistingServer: !process.env.CI` — local runs reuse the Docker stack between test runs
- `e2e/teardown.ts` checks `process.env.CI` and skips teardown locally, so the stack stays up
- **Impact:** iterating on a single test file takes ~4s instead of ~4min (full rebuild)

**Auth fixture pattern:**
- Uses API-based auth (register + login via `/api/auth/*`)
- CSRF token from `csrf_` cookie required for POST requests
- `authenticatedPage` and `adminPage` fixtures auto-login
- Rate limit set to 1000 req/min in `docker-compose.e2e.yml` for test stability

**CRITICAL auth gotcha:** `getCsrfToken()` is async (`Promise<string>`). It **must** be `await`ed:
```typescript
const csrfToken = await getCsrfToken(page);  // ✅ correct
const csrfToken = getCsrfToken(page);          // ❌ csrfToken is "[object Promise]" → every request gets 403
```
This is the #1 cause of auth-related E2E failures. `auth.spec.ts` historically had 29 call sites missing `await` (DJI-441).

**CRITICAL: always run from `e2e/` directory.** `npx playwright` from the repo root resolves to a globally-installed playwright (v1.61.0), which mismatches the local `@playwright/test` (v1.61.1) and produces the cryptic error `"test.describe() called from async test.describe() block"`.

**After backend/frontend code changes,** rebuild the Docker stack before re-running E2E tests (the stack runs compiled binaries, not live source):
```bash
docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml up -d --build
```

**CI:** `.github/workflows/e2e.yml` runs on push/PR to main/develop

## API / Interface Reference

### HTTP API (Fiber)

#### Public Endpoints

| Route | Method | Auth required | Handler |
|---|---|---|---|
| `/api/health` | GET | No | `HealthHandler.GetHealth` (probes DB + slskd + Gonic) |
| `/api/auth/register` | POST | No + rate-limited | `AuthHandler.Register` |
| `/api/auth/login` | POST | No + rate-limited | `AuthHandler.Login` |
| `/api/auth/logout` | POST | Session recommended | `AuthHandler.Logout` |

#### UI Pages (Fiber HTML — Pongo2 templates)

| Route | Method | Auth required | Handler |
|---|---|---|---|
| `/` | GET | No | `DashboardHandler.RenderIndex` |
| `/watchlists` | GET | Yes | `WatchlistHandler.WatchlistsPage` |
| `/libraries` | GET | Yes | `LibraryHandler.LibrariesPage` |
| `/profiles` | GET | Yes | `ProfileHandler.ProfilesPage` |
| `/schedules` | GET | Yes | `SchedulesHandler.SchedulesPage` |
| `/artists` | GET | Yes | `ArtistsHandler.ArtistsPage` |
| `/jobs` | GET | Yes | `StatsHandler.JobsPage` |

#### HTMX Partials (all protected)

| Route | Method | Handler |
|---|---|---|
| `/partials/stats` | GET | `StatsHandler.RenderStatsPartial` |
| `/partials/watchlists` | GET | `WatchlistHandler.RenderWatchlistsPartial` |
| `/partials/libraries` | GET | `LibraryHandler.RenderLibrariesPartial` |
| `/partials/schedules` | GET | `SchedulesHandler.RenderSchedulesPartial` |
| `/partials/artists` | GET | `ArtistsHandler.RenderPartial` |
| `/partials/artist-form` | GET | `ArtistsHandler.GetForm` |
| `/partials/jobs` | GET | `StatsHandler.RenderJobsPartial` |
| `/partials/libraries/:id/browse` | GET | `LibraryHandler.BrowseTracks` |
| `/partials/tracks/:id` | GET | `LibraryHandler.TrackDetail` |

#### Protected API (Fiber JSON)

| Route | Method | Auth required | Handler |
|---|---|---|---|
| `/api/auth/spotify/login` | GET | Yes | `SpotifyAuthHandler.Login` |
| `/api/auth/spotify/callback` | GET | Yes | `SpotifyAuthHandler.Callback` |
| `/api/auth/spotify/spdc` | POST | Yes | `SpotifyAuthHandler.LinkSpDc` — stores sp_dc browser cookie for GraphQL Partner API access |
| `/api/watchlists/*` | GET/POST/PATCH/DELETE | Yes | `WatchlistHandler` + preview handler |
| `/api/profiles/*` | GET/POST/PATCH/DELETE | Yes (admin for writes/default) | `ProfileHandler` |
| `/api/artists/*` | GET/POST/PATCH/DELETE | Yes | `ArtistsHandler` |
| `/api/schedules/*` | GET/POST/PATCH/DELETE | Yes | `SchedulesHandler` |
| `/api/libraries/*` | GET/POST/PATCH/DELETE | Yes | `LibraryHandler` |
| `/api/libraries/:id/{scan,enrich,prune}` | POST | Yes | `LibraryHandler.Trigger{Scan,Enrich,Prune}` |
| `/api/libraries/:id/tracks` | GET | Yes | `LibraryHandler.ListTracks` |
| `/api/stats/*` | GET | Yes | `StatsHandler` (jobs, jobs/breakdown, jobs/trends, library, activity, summary) |
| `/api/jobs` | GET | Yes | inline query |
| `/api/jobs/sync` | POST | Yes | inline queue trigger |
| `/api/jobs/:id/retry` | POST | Yes | `agent.RetryJob` |
| `/api/jobs/:id/cancel` | POST | Yes | `agent.CancelJob` |
| `/api/artists/track` | POST | Yes | inline handler |
| `/api/library/scan` | POST | Yes | inline handler |
| `/ws/events` | GET (WebSocket) | Yes | `WebSocketManager.HandleEvents` |
| `/ws/jobs/:job_id` | GET (WebSocket) | Yes | `WebSocketManager.HandleConsole` |

> Note: `*` wildcard routes cover CRUD operations plus sub-routes like `form`, `toggle`, `preview`, and `profiles` under the same group.

### CLI (`netrunner-cli`)

| Command | Signature | Result |
|---|---|---|
| `status` | `netrunner-cli status` | System probe summary |
| `config` | `netrunner-cli config list` | Current non-sensitive config |
| `watchlist` | `list`, `add [name] [type] [uri]`, `sync [id]`, `import` | Watchlist management |
| `library` | `list`, `add [name] [path]`, `scan [id]`, `prune [id]`, `rm [id]` | Library lifecycle |
| `profile` | `list`, `add [name]`, `rm [id]`, `set-default [id]` + flags | Quality profile lifecycle |
| `stats` | `summary`, `jobs`, `library` | Stats reports |

### MCP Tools (`backend/cmd/agent`)

> MCP surface is stable as of v0.0.1. Tool names and schemas below are the source of truth.

| Tool | Input | Output | Side Effects | Idempotent |
|---|---|---|---|---|
| `probe_system` | _(none)_ | `{database_connected, gonic_connected, slskd_connected, message}` | None (read-only) | Yes |
| `read_config` | _(none)_ | `map[string]string` of non-sensitive settings | None (read-only) | Yes |
| `update_config` | `key: string` (required), `value: string` (required) | Success message | Upserts `Setting` row | No |
| `list_watchlists` | _(none)_ | `[]Watchlist` with name, source_type, source_uri, enabled | None (read-only) | Yes |
| `add_watchlist` | `name: string` (required), `source_type: string` (required), `source_uri: string` (required), `quality_profile_id: UUID` (required) | Created watchlist with ID | Creates `Watchlist` row | No |
| `sync_watchlist` | `watchlist_id: UUID` (required) | Queued job ID | Creates `Job` row (type=sync) | No |
| `list_jobs` | `limit: number` (optional, default 10) | `[]Job` with state, type, requested_at | None (read-only) | Yes |
| `get_job_logs` | `job_id: number` (required) | `[]JobLog` with timestamp, level, message | None (read-only) | Yes |
| `enqueue_acquisition` | `artist: string` (required), `title: string` (required), `album: string` (optional) | Queued job ID | Creates `Job` + `JobItem` rows | No |
| `bootstrap` | _(none)_ | `map[string]string` of check results | Runs `AutoMigrate` | Yes (safe to retry) |
| `search_library` | `query: string` (required) | `[]match` with artist, title, album, source | None (read-only) | Yes |
| `register_webhook` | `url: string` (required) | Success message | Upserts webhook setting | No |
| `get_stats` | _(none)_ | Jobs (24h), library, activity summary | None (read-only) | Yes |
| `list_quality_profiles` | _(none)_ | `[]QualityProfile` with settings | None (read-only) | Yes |
| `list_libraries` | _(none)_ | `[]Library` with name, path, ID | None (read-only) | Yes |
| `scan_library` | `library_id: UUID` (required) | Queued job ID | Creates `Job` row (type=scan) | No |
| `add_library` | `name: string` (required), `path: string` (required) | Created library with ID | Creates `Library` row | No |
| `list_monitored_artists` | _(none)_ | `[]MonitoredArtist` with MBID, release counts | None (read-only) | Yes |
| `cancel_job` | `job_id: string` (required, numeric) | Success message | Updates job + items to cancelled | No |
| `retry_job` | `job_id: string` (required, numeric) | Success message | Resets failed items to queued | No |

**Error behavior**: All tools return `mcp.CallToolResultError` with a descriptive message on failure. No partial state is left on error for read-only tools. Write tools may leave a partially created row if the DB operation succeeds but a follow-up fails.

### Internal Abstractions

- `interfaces.WatchlistProvider`
  - `FetchTracks(ctx, watchlist) ([]map[string]string, string, error)`
  - `ValidateConfig(config string) error`
- `interfaces.SpotifyClientProvider`
  - `GetClient(ctx, userID) (*spotify.Client, error)`

## Data Models
Key entities (GORM, mostly in `backend/internal/database/models.go`):

- `User` (1:N `Session`): session-auth principals, role field (`user`/`admin`)
- `QualityProfile`: acquisition policy (formats, bitrate, lossless preference, filter mode)
- `Watchlist`: source config + quality profile linkage
- `Schedule`: cron sync config per watchlist
- `Job` (1:N `JobItem`, 1:N `JobLog`): background execution state machine
- `Acquisition`: imported artifact provenance and metadata linkage
- `Library` (1:N `Track`): filesystem collection roots + quota fields
- `MonitoredArtist` (1:N `TrackedRelease`): artist release tracking
- `MetadataCache`: API response cache with expiry
- `SpotifyToken`: per-user Spotify OAuth tokens (access + refresh)
- `Lock`: distributed advisory lock for session-level exclusivity
- `Setting`: dynamic key-value runtime config
- `PeerReputation`: Soulseek peer quality tracking for scoring

Schema/migration sources:
- SQL bootstrap: `ops/db/init/01-schema.sql`, `ops/db/init/02-functions.sql`
- Forward SQL migrations: `ops/db/init/migrations/*.sql`
- Runtime migration path: `database.Migrate(db)` + `AutoMigrate`

## Testing Guide
- Test files follow `*_test.go`, primarily under `backend/internal/*` and `backend/cmd/*`.
- Default runner:
  - `cd backend && go test ./...`
- Targeted package tests:
  - `go test ./internal/api -run TestAuth -v`
  - `go test ./cmd/worker -run TestRoundRobin -v`
- Integration-tagged tests:
  - `go test ./internal/integration/... -tags=integration -v`
  - or `./scripts/integration-tests.sh test`
- Coverage:
  - CI uses `go test ./... -coverprofile=coverage.out`

## Database Driver Behavior

NetRunner supports SQLite WAL and PostgreSQL. The database driver is auto-detected from `DATABASE_URL`:

| Behavior | SQLite WAL | PostgreSQL |
|---|---|---|
| Connection string prefix | No prefix / ends in `.db` | `postgres://` or `postgresql://` |
| Journal mode | WAL (set via PRAGMA) | WAL by default |
| Advisory locks (`pg_try_advisory_lock`) | Not available — file locking only | Full support via `PostgresLockManager` |
| Job wakeup | Polling interval | `LISTEN/NOTIFY` support |
| Concurrent workers | Unsafe — single writer only | Safe — connection pooling + advisory locks |
| `LiteFSGuard` primary detection | Checks `/litefs/.primary` file | Always primary (guard is no-op) |
| `FILTER (WHERE ...)` SQL syntax | Not supported — agent stats queries will fail | Full support |
| Schema migrations | GORM `AutoMigrate` only | SQL bootstrap (`ops/db/init/`) + `AutoMigrate` |

**Important**: If `MaxConcurrentJobs > 1` and SQLite is detected, the worker emits a startup warning. Use Postgres for production concurrent workloads.

### Advisory Lock Implementation

The `WorkerOrchestrator` acquires advisory locks per scope ID before processing jobs to prevent concurrent operations on the same watchlist or library.

| Driver | Lock Manager | Mechanism | Concurrent Safety |
|---|---|---|---|
| PostgreSQL | `PostgresLockManager` | `pg_try_advisory_lock` / `pg_advisory_unlock` — true session-level mutual exclusion | Safe — different connections get real mutual exclusion |
| SQLite | `TableLockManager` | `locks` table with `key` + `expires_at` — check-then-insert inside a GORM transaction | **Single-worker only** — SQLite's transaction serialization provides basic protection, but is not equivalent to Postgres advisory locks under high concurrency. Locks expire after 15 minutes as a safety net. |

**SQLite is single-worker only.** The `TableLockManager` uses row-level locking emulated via a table, which provides basic mutual exclusion within a single process due to SQLite's serialized writes. However, it does not provide cross-process locking and is not safe for multi-worker deployments. Always use PostgreSQL when running multiple workers.

## Common Agent Tasks

### 1) Add a new feature
1. Locate the owning layer:
   - API contract: `backend/internal/api/`
   - business logic: `backend/internal/services/`
   - data shape: `backend/internal/database/`
2. Implement minimal path:
   - route/handler + service + model updates as needed
3. Add/extend tests:
   - handler tests in `backend/internal/api/*_test.go`
   - service tests in `backend/internal/services/*_test.go`
4. Validate:
   - `cd backend && go vet ./...`
   - `cd backend && go test ./...`

### 2) Fix a bug
1. Reproduce with targeted test:
   - `cd backend && go test ./<package> -run <TestName> -v`
2. Trace request/data path through handler -> service -> database.
3. Patch smallest safe unit and add regression test.
4. Re-run package tests, then broader suite.

### 3) Run a migration/schema change
1. Update model(s) in `backend/internal/database/models.go`.
2. If SQL-specific transformation is needed, add file in `ops/db/init/migrations/`.
3. Ensure `database.Migrate(db)` handles transition safely.
4. Validate with sqlite/postgres test path (as available).

### 4) Update a dependency
1. Edit module refs:
   - `cd backend && go get <module>@<version>`
   - `go mod tidy`
2. Re-run `go vet`, `go test`, and `go build`.
3. Run vulnerability scan:
   - `govulncheck ./...`

### 5) Deploy or run full stack
1. Set required env vars in `.env`.
2. Build and launch:
   - `docker compose up -d --build`
3. Validate health:
   - `curl http://localhost:8080/api/health`
4. Inspect logs:
   - `docker compose logs -f netrunner`

## Pitfalls & Gotchas
- `<!-- OUTDATED: prior guide states JWT+RBAC. Current implementation uses session cookie auth via sessions table, with role checks in handlers/services. -->`
- `<!-- OUTDATED: prior guide states Redis-backed rate limiting is mandatory. Current auth limiter in server setup uses Fiber limiter defaults and does not wire Redis storage. -->`
- `<!-- OUTDATED: prior guide references Nginx ownership in ops. Current reverse proxy config is Caddy (`ops/caddy/Caddyfile`). -->`
- `<!-- OUTDATED: prior guide says avoid hardcoded role names absolutely. Current code still has explicit role checks (e.g., "admin") in handlers; keep behavior consistent unless performing coordinated RBAC refactor. -->`
- <!-- RESOLVED 2026-05-19: library test path failures fixed in commit 1250fe4. All tests pass cross-platform. -->
- `database.Migrate` contains PostgreSQL enum-to-text conversions; read it before modifying job state columns.
- `backend/entrypoint.sh` is a single-process bootstrap for Docker (creates dirs, logs, exec). The Docker Compose stack runs `ops-web` and `ops-worker` as separate services with different `command` overrides. For local debugging, run binaries directly with `go run ./cmd/server` or `go run ./cmd/worker`.
- Spotify OAuth defaults callback to `http://localhost:8080/api/auth/spotify/callback` unless `SPOTIFY_REDIRECT_URI` is set.
- **Pongo2 renders Go bool as capitalized `True`/`False`**, not lowercase `true`/`false`. Use `{% if field %}true{% else %}false{% endif %}` to get lowercase. E.g., `Lossless: True` (capital T) broke E2E tests expecting `Lossless: true`.
- **Pongo2 `{% if ID %}` is always true for UUID fields.** The zero UUID `00000000-0000-0000-0000-000000000000` is a non-empty string, so `{% if ID %}` evaluates to true. Always pass an explicit `IsNew` bool when the template needs to distinguish "add" from "edit".
- **Go `encoding/json` silently drops fields without JSON tags.** `source_uri` (snake_case) does NOT match `SourceURI` (PascalCase) unless the struct has `json:"source_uri"`. The case-insensitive fallback only works for pure case differences, not underscore differences. Both the model AND the input struct need JSON tags (DJI-437).
- **HTMX only swaps 2xx responses.** Error paths returning 4xx/5xx are silently discarded by HTMX — the target div stays unchanged. Always check `isHTMXRequest(c)` and return `c.SendString("<div class=\"error\">...</div>")` on error paths for HTMX requests (DJI-438).
- **API responses use PascalCase field names** (`ID`, `Name`, `SourceType`, `IsDefault`), not camelCase. E2E tests must check `response.ID` not `response.id`.
- **Multiple create handlers don't close the modal after HTMX submit.** The JS listens for `HX-Trigger: closeModal` header, but only `AcquireHandler.Create` sets it. Any new form handler needs `c.Set("HX-Trigger", "closeModal")` before returning the partial (DJI-440).

## Skill Index
- `release-readiness-review` — Two-phase audit+closure pattern for shipping releases. See `.agents/skills/release-readiness-review/SKILL.md`.
- `e2e-test-spec-generator` — Generate Playwright E2E test specs from Linear issue descriptions.
- `auto-linear-update` — Automatically update Linear issues with E2E test results after test runs.
- See `.agents/skills/` for additional project-specific skills.

## Cloned Dependency Source

Read-only dependency source repositories are available under
`.slim/clonedeps/repos/` for inspection. Do not edit these clones.

- `.slim/clonedeps/repos/gofiber__fiber/` — **gofiber/fiber** at `v2.52.13`; HTTP framework source for debugging middleware chain, context methods, error handling, and route matching.
- `.slim/clonedeps/repos/go-gorm__gorm/` — **go-gorm/gorm** at `v1.31.1`; ORM source for debugging query building, preloading, transaction behavior, and migration patterns.
- `.slim/clonedeps/repos/mark3labs__mcp-go/` — **mark3labs/mcp-go** at `v0.45.0`; MCP protocol Go SDK for debugging tool definitions, protocol messages, and agent transport layer.

# Agents Guide - NetRunner

> Last updated by Codex ingestion: 2026-05-07

## Overview
NetRunner is a Go-based music acquisition and library-operations platform. It ingests tracks from watchlist sources (Spotify, Last.fm, ListenBrainz, RSS, local files), orchestrates acquisition jobs through slskd (Soulseek daemon), enriches metadata, and maintains local music libraries with a Fiber + HTMX UI.

Agents working in this repository should assume read-write autonomy for local code and docs updates, test execution, and non-destructive tooling changes. Runtime/deployment actions (docker compose changes, production environment changes, credential rotation) should be treated as operator-reviewed actions.

Current authentication is session-cookie based (`session_id`) with role checks (`user`, `admin`) at handler/service boundaries. This differs from the older JWT-first description and should be considered the source of truth for API behavior.

## Repository Map

| Directory | Type | Purpose | Agent-relevant? |
|---|---|---|---|
| `backend/cmd/` | Go executables | Entry points for `server`, `worker`, `cli`, `agent`, `test_sqlite` | Yes |
| `backend/internal/api/` | HTTP layer | Fiber handlers, route logic, auth/session middleware, HTMX/WebSocket endpoints | Yes |
| `backend/internal/services/` | Service layer | Acquisition orchestration, providers, API clients, metadata/tagging, notifications | Yes |
| `backend/internal/database/` | Data layer | GORM models, connection, migrations, lock managers, helpers | Yes |
| `backend/internal/agent/` | Agent facade | Transport-agnostic functions used by MCP server + CLI | Yes |
| `backend/internal/config/` | Config | Env loading and defaults, required/optional config validation | Yes |
| `backend/internal/interfaces/` | Abstractions | `WatchlistProvider` and `SpotifyClientProvider` contracts | Yes |
| `backend/internal/integration/` | Integration tests | Dockerized slskd/integration harness and test scenarios | Yes |
| `ops/` | Infra config | Docker Compose, Caddy, DB init schema/functions/migrations, web assets | Yes |
| `.github/workflows/` | CI/CD | Go CI (`go vet`, `go test`, coverage artifact) | Yes |
| `scripts/` | Ops scripts | Integration test orchestration helper (`integration-tests.sh`) | Yes |
| `docs/` | Documentation | Architecture/plans/runbooks | Yes |
| `conductor/` | Docs/archive | Legacy/archived docs area | Sometimes |
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

| VAR_NAME | Required | Default | Purpose |
|---|---|---|---|
| `ENVIRONMENT` | No | `development` | Runtime environment label |
| `PORT` | No | `8080` | HTTP server port |
| `DOMAIN` | No | `localhost` | Caddy host/domain |
| `JWT_SECRET` | Recommended | auto-generated random secret if unset | Session/auth crypto secret; set explicitly for stable restarts |
| `DATABASE_URL` | Yes | none | Primary DB connection string (`postgres://...` or SQLite path) |
| `REDIS_URL` | No | `redis://localhost:6379` | Reserved for redis-backed features/future middleware |
| `SPOTIFY_CLIENT_ID` | Conditional | empty | Spotify OAuth/API integration |
| `SPOTIFY_CLIENT_SECRET` | Conditional | empty | Spotify OAuth/API integration |
| `SPOTIFY_REDIRECT_URI` | No | `http://localhost:8080/api/auth/spotify/callback` | OAuth callback URL override |
| `MUSICBRAINZ_USER_AGENT` | No | `NetRunner/1.0.0 (contact@example.com)` | MusicBrainz request identity |
| `MUSICBRAINZ_API_KEY` | No | empty | Optional MusicBrainz key |
| `ACOUSTID_API_KEY` | No | empty | AcoustID fingerprint lookup |
| `SLSKD_URL` | No | `http://localhost:5030` | slskd API base URL |
| `SLSKD_API_KEY` | Yes (for acquisition features) | empty | slskd API auth |
| `GONIC_URL` | No | `http://localhost:4747` | Gonic/Subsonic API URL |
| `GONIC_USER` | Conditional | empty | Gonic auth user |
| `GONIC_PASS` | Conditional | empty | Gonic auth password |
| `MUSIC_LIBRARY` | No | `./music_library` | Library root path |
| `TEMPLATES_PATH` | No | `./ops/web/templates` | Pongo2 template directory |
| `STATIC_FILES_PATH` | No | `./ops/web/static` | Static files directory |
| `LASTFM_API_KEY` | No | empty | Last.fm provider access |
| `LISTENBRAINZ_TOKEN` | No | empty | ListenBrainz provider token |
| `DISCOGS_TOKEN` | No | empty | Discogs API token |
| `PROXY_URL` | No | empty | Outbound proxy for service calls |
| `NOTIFICATION_ENABLED` | No | `false` | Enable webhook notifications |
| `NOTIFICATION_WEBHOOK_URL` | Conditional | empty | Notification sink URL |
| `AUTH_RATE_LIMIT_MAX` | No | `10` | Auth endpoint rate-limit max requests |
| `AUTH_RATE_LIMIT_EXPIRATION` | No | `1m` | Auth rate-limit window |
| `POSTGRES_PASSWORD` | Docker-only | `changeme` fallback | Postgres password for compose stack |
| `SLSKD_USERNAME` | Docker-only | none | slskd Soulseek username |
| `SLSKD_PASSWORD` | Docker-only | none | slskd Soulseek password |
| `SLSKD_TEST_USERNAME` | Integration-only | `testuser` | Integration slskd user |
| `SLSKD_TEST_PASSWORD` | Integration-only | `testpass` | Integration slskd password |
| `SLSKD_TEST_API_KEY` | Integration-only | `test-api-key-for-integration` | Integration slskd API key |
| `INTEGRATION_DB_PASSWORD` | Integration-only | `testpass` | Integration Postgres password |
| `INTEGRATION_SLSKD_URL` | Integration-only | `http://localhost:15030` | Integration test slskd URL |
| `INTEGRATION_SLSKD_API_KEY` | Integration-only | `test-api-key-for-integration` | Integration test API key |
| `INTEGRATION_DATABASE_URL` | Integration-only | `postgresql://testuser:testpass@localhost:15432/netrunner_integration?sslmode=disable` | Integration DB connection |
| `INTEGRATION_SLSKD_USERNAME` | Integration-only | empty | Optional download-flow test user |
| `INTEGRATION_SLSKD_PASSWORD` | Integration-only | empty | Optional download-flow test password |
| `SKIP_INTEGRATION_TESTS` | Integration-only | empty | Skip integration tests when `true` |
| `SKIP_NETWORK_TESTS` | Test-only | empty | Skip network-dependent tests |

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
| `govulncheck ./...` | Vulnerability scan for reachable issues in code + deps | Security/dependency maintenance |

## API / Interface Reference

### HTTP API (Fiber)

| Route | Method | Auth required | Handler |
|---|---|---|---|
| `/api/health` | GET | No | inline health handler |
| `/api/auth/register` | POST | No + rate-limited | `AuthHandler.Register` |
| `/api/auth/login` | POST | No + rate-limited | `AuthHandler.Login` |
| `/api/auth/logout` | POST | Session recommended | `AuthHandler.Logout` |
| `/api/auth/spotify/login` | GET | Yes | `SpotifyAuthHandler.Login` |
| `/api/auth/spotify/callback` | GET | Yes | `SpotifyAuthHandler.Callback` |
| `/api/watchlists/*` | GET/POST/PATCH/DELETE | Yes | `WatchlistHandler` + preview handler |
| `/api/profiles/*` | GET/POST/PATCH/DELETE | Yes (admin for writes/default) | `ProfileHandler` |
| `/api/artists/*` | GET/POST/PATCH/DELETE | Yes | `ArtistsHandler` |
| `/api/schedules/*` | GET/POST/PATCH/DELETE | Yes | `SchedulesHandler` |
| `/api/libraries/*` | GET/POST/PATCH/DELETE | Yes | `LibraryHandler` |
| `/api/stats/*` | GET | Yes | `StatsHandler` |
| `/api/jobs/sync` | POST | Yes | inline queue trigger |
| `/ws/events` | GET (WebSocket) | Yes | `WebSocketManager.HandleEvents` |
| `/ws/jobs/:job_id` | GET (WebSocket) | Yes | `WebSocketManager.HandleConsole` |

### CLI (`netrunner-cli`)

| Command | Signature | Result |
|---|---|---|
| `status` | `netrunner-cli status` | System probe summary |
| `config` | `netrunner-cli config list` | Current non-sensitive config |
| `watchlist` | `list`, `add [name] [type] [uri]`, `sync [id]`, `import` | Watchlist management |
| `library` | `list`, `add [name] [path]`, `scan [id]`, `rm [id]` | Library lifecycle |
| `profile` | `list`, `add [name]`, `rm [id]`, `set-default [id]` + flags | Quality profile lifecycle |
| `stats` | `summary`, `jobs`, `library` | Stats reports |

### MCP Tools (`backend/cmd/agent`)

`probe_system`, `read_config`, `update_config`, `list_watchlists`, `add_watchlist`, `sync_watchlist`, `list_jobs`, `get_job_logs`, `enqueue_acquisition`, `bootstrap`, `search_library`, `register_webhook`, `get_stats`, `list_quality_profiles`, `list_libraries`, `scan_library`, `add_library`, `list_monitored_artists`, `cancel_job`, `retry_job`.

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

Known caveat (2026-05-07, resolved 2026-05-19):
- ~~`go test ./...` currently fails in `internal/api` on path-validation expectation mismatches in `libraries_test.go` on this Windows workspace.~~
- **RESOLVED**: Commit `1250fe4` fixed cross-platform path handling in `libraries_test.go`. All tests pass on both Windows and Linux.

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

## Skill Index
- `skills/repo-setup.md` - End-to-end local environment setup and first successful validation run.
- `skills/run-tests.md` - Unit/integration test execution patterns and failure triage.
- `skills/add-feature.md` - Canonical feature-delivery workflow for this repo.
- `skills/fix-bug.md` - Structured bug reproduction, isolation, patching, and verification.
- `skills/data-model.md` - Schema/model update workflow across GORM + SQL migrations.
- `skills/api-usage.md` - HTTP API, CLI, and MCP interface usage patterns.
- `skills/deploy.md` - Build/package/deploy and rollback checks for Docker/Caddy stack.
- `skills/debugging.md` - Logging, tracing, worker/job diagnostics, and common probes.
- `skills/dependency-management.md` - Dependency update, lock resolution, and vulnerability scanning.
- `skills/watchlist-operations.md` - Watchlist lifecycle and sync workflow.
- `skills/artist-monitoring.md` - Monitored artist and release-tracking workflow.
- `skills/acquisition-pipeline.md` - Queue-to-import acquisition pipeline operations and validation.

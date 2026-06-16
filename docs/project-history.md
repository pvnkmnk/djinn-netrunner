# NetRunner — Project History & State

**Last comprehensive audit:** 2026-05-19

---

## What NetRunner Is

NetRunner is a Go-based music acquisition and library-management platform. It ingests tracks from watchlist sources (Spotify, Last.fm, ListenBrainz, RSS, local files), orchestrates acquisition jobs through slskd (Soulseek daemon), enriches metadata (MusicBrainz, AcoustID, Discogs), and maintains local music libraries with a Fiber + HTMX UI.

### Architecture at a Glance

| Layer | Technology | Role |
|---|---|---|
| HTTP server | Go / Fiber | REST API + server-rendered HTMX UI + WebSocket |
| Background worker | Go | Job orchestrator, pipeline executor |
| Database | SQLite (WAL) / PostgreSQL | Jobs, logs, metadata, users, config |
| Reverse proxy | Caddy | TLS, CSP headers |
| Acquisition daemon | slskd | Soulseek downloads |
| Streaming server | gonic | Subsonic-compatible library streaming |
| Agent interface | MCP over stdio | AI agent tool integration |

### Authentication & Authorization

Session-cookie based (`session_id` cookie, `sessions` table). Role checks (`user`, `admin`) at handler/service boundaries. Rate-limited auth endpoints.

---

## Full Cleanup — 2026-05-19

### Branch Cleanup: 65 Remotes → 1

Before this session, the remote tracked **65 branches** — a mix of merged, abandoned, and stale feature branches accumulated over 3+ months. Every branch was audited:

| Category | Count |
|---|---|
| Fully merged (fast-forward or 3-way) | 10 |
| Squash-merged (zero unique commits via `git cherry`) | 3 |
| Abandoned experiment branches (bolt, palette, sentinel, tembo, fix-bola, revert, tessl, phase-0) | 52 |
| Stale feature branches (already on master) | 2 |
| **Total deleted** | **67** |
| **Preserved** | **0** |

The 2 feature branches (`feature/phase-1-security-hardening`, `feature/phase-2-pipeline-architecture`) were reviewed commit-by-commit against master. **Every change was already on master** — including SSRF-safe HTTP client, CSP headers, HTMX local vendoring, OAuth CSRF, CI workflow, context cancellation, Browse()/rateLimiter, the 7-stage acquisition pipeline, PeerReputation model, and extended QualityProfile fields. Both were deleted.

**Result:** Remote is `master` only.

### Cherry-Picks Applied

From `fix/handle-database-errors` (a squash-merged branch whose unique commits never reached master):

1. Test cleanup style alignment
2. Nil-safe slice dereference in test helpers
3. Graceful database skip in test isolation
4. In-memory SQLite for test isolation
5-6. Additional test infrastructure improvements

### Stash Cleanup

3 stashes dropped. All contents verified already present on master:
- Stash 0: Sentinel BOLA fix + rate limiter + Caddy proxy + Docker non-root user
- Stash 1: Modal CSS
- Stash 2: Initial repo snapshot

### Linear Issue Audit

Linear workspace has **83 issues** total. Breakdown:

| Project | Issues | Status |
|---|---|---|
| vault-memory | 71 active | Mostly open, in current cycle |
| netrunner security vulns (DJI-36 through DJI-225) | 12 | All **Duplicate/Canceled** |
| **Open netrunner-specific issues** | **0** | — |

The current cycle (Cycle 4) has 10 issues at 0% completion — all vault-memory, no netrunner items.

### Linear MCP Configuration

Fixed dual-config conflict. Both `opencode.json` files (project-level + global) now use the same verified remote HTTP endpoint with `oauth: false`. The stale local npx-based config with a different API key was removed.

---

## Implementation History

### Phase 1–3: Foundation (PRs #22–24)
- Go backend initialization
- Database models and migrations
- MusicBrainz, slskd, Gonic service skeletons
- Worker orchestrator with job claiming

### Phase 4: Library Scanner (PRs #23–24)
- ScannerService for tag extraction
- Metadata enrichment pipeline
- Acoustic fingerprinting via AcoustID

### Phase 5: Quality Profiles (PR #25)
- CRUD API for quality profiles
- Bitrate/format/priority preferences
- Profile assignment per watchlist

### Phase 6: UI Implementation (PR #26)
- Full management UI with HTMX + Fiber
- Dashboard, watchlists, artists, schedules, libraries, profiles, jobs
- Cyberpunk glassmorphic theme
- WebSocket console with per-job streaming

### Phase 7: Hardening & Polish (PR #32)
- slskd health check in MCP system status
- Ambiguous artist search logging
- WebSocket per-job broadcast filtering
- Background Spotify token refresh
- Job completion webhook notifications
- MusicBrainz cover art fallback
- Watchlist preview endpoint
- Integration test expansion

### Phase 8+ (Now on Master)
The following work from planned Phase 8 and Linear roadmap items was verified as **already merged**:

**Security Hardening:**
- SSRF-safe HTTP client (`pkg/safe_http/`)
- CSP headers (Caddy)
- HTMX local vendoring (no CDN)
- OAuth state CSRF protection
- User enumeration hardening
- CI workflow (`.github/workflows/go-ci.yml`)
- Docker compose port cleanup
- Worker `context.Context` cancellation
- Race flag removal

**Pipeline Architecture:**
- Browse()/rateLimiter/deleteSearch in slskd service
- 7-stage acquisition pipeline (`stageLoadItemContext` → `stageImportAndEnrich`)
- PeerReputation scoring model
- Extended QualityProfile fields (FormatPreferenceOrder, FilterMode)

---

### Cycle A: Foundation & CI/CD (PR #37)
Cycle A targeted the foundation layer — Docker packaging, CI/CD, dependency health, and operational readiness:

**DJI-303:** Split server/worker in Docker Compose. Simplified `entrypoint.sh` to single-process bootstrap (create dirs, exec). Added `CMD ["./netrunner-server"]` default. Split `docker-compose.yml` into `ops-web` + `ops-worker` services sharing the same build image but different `command` overrides.

**DJI-305:** Dependency audit. `govulncheck` found one uncalled vulnerability in `golang.org/x/net` v0.52.0 (HTTP/2 infinite loop, GO-2026-4918). Bumped to v0.53.0, with transitive bumps for `x/crypto` v0.49.0→v0.50.0, `x/sys` v0.42.0→v0.43.0, `x/text` v0.35.0→v0.36.0. `go mod tidy` clean.

**DJI-302:** Automated Docker builds via `.github/workflows/docker.yml`. Builds and pushes to GHCR on main branch pushes and v* tags. Uses Buildx + layer caching via `type=gha`. Tags with semver, branch name, and `latest`.

**DJI-304:** Enhanced `/api/health` endpoint. Moved from inline handler to `internal/api/health.go` following the `StatsHandler` pattern. Returns per-dependency checks (database, slskd, gonic, disk) with an overall `"ok"` or `"degraded"` status.

**DJI-306:** Made `govulncheck` blocking in CI. Removed `continue-on-error: true` from the vulnerability scan step — the pipeline now fails on reachable vulnerabilities.

**DJI-307:** Docker Compose health checks. Added `wget`-based healthcheck for `ops-web`, `kill -0 1` process check for `ops-worker`. Caddy now depends on `condition: service_healthy` for `ops-web`.

---

## Current State

### Working
- [x] HTTP API (Fiber, all routes functional)
- [x] Session auth + role checks
- [x] Watchlist CRUD + sync (Spotify, Last.fm, ListenBrainz, RSS, local)
- [x] Artist monitoring + release tracking (MusicBrainz)
- [x] Acquisition pipeline (Soulseek via slskd)
- [x] Library scanning + metadata enrichment
- [x] Quality profiles
- [x] WebSocket console streaming
- [x] MCP agent interface (20 tools)
- [x] CLI interface
- [x] CI pipeline (go vet + test + coverage)
- [x] Caddy reverse proxy with CSP

### Known Issues
- [x] Legacy `conductor/` directory removed (Cycle C cleanup)

---

## References

See individual files for detailed contracts:

| File | What |
|---|---|
| `AGENTS.md` | Agent operating guide, env vars, commands |
| `docs/ARCHITECTURE.md` | Service architecture, data model, API routes |
| `docs/RUNBOOK.md` | Operations runbook |
| `backend/internal/database/models.go` | All GORM models |
| `backend/internal/api/` | HTTP handlers |
| `backend/internal/services/` | Business logic |

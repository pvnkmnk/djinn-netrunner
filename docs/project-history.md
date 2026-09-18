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

## Beta Hardening — September 2026 (PRs #202-#244)

The repository had a feature-complete UI and a working acquisition pipeline that had
never been proven on a real deployment. This wave is what it took to get from "all CI
green" to "a beta that acquires, imports, streams and repairs a real library", and
most of it came from running the thing and hitting what the tests did not.

**What ran, and why.** An end-to-end run against the operator's Docker Desktop stack
found that a vanilla `docker compose up` never received `.env` (random session secret,
no Subsonic password), that slskd rejected every search with 401, and that no
media-server client existed in the worker (#226, #229, #470-series issues). Each of
those was invisible to a green test suite because the tests construct their own
configuration.

**What the pipeline got wrong with real peers.** A peer that answered a search and
never sent anything stalled the queue for the full wait budget (#228); an 8.5 KB
"track" was imported (#228); and an unrelated recording whose filename contained the
requested word was imported under a green acceptance run — the finding that produced
the download-identity gate, now on both entrances into the import stage (#239, #240).

**What the library got wrong.** Albums were fragmented across per-credit artist
folders (#219), then across case-only differences (#230, #231); a repaired folder
still carried the wrong tag, so a Subsonic client listed one artist twice (#241). Each
fix came with the repair tooling for libraries already built: the album merge (#222,
#224) and `library repair-tags` (#241), both dry-run first and backed up.

**What staging accumulated.** Leftover directories survived because the sweep only
removed empty ones (#231), a permissions mismatch stopped slskd writing at all (#220),
and every non-import exit leaked its download until a single owning discard path and a
janitor that reclaims unowned entries replaced the per-branch removals (#233, #234,
#235, #236).

**What was made verifiable.** `docs/BETA_ACCEPTANCE.md` records each clause with the
command that tested it and the output it observed (#229, #232, #241); the browser suite
became a real gate that runs in CI and locally, with the four missing spec files added
and the e2e stack given its own compose identity so it cannot touch a beta deployment
(#244).

**Decisions recorded rather than deferred.** `jobitems.source_url` had no production
writer, so the yt-dlp fallback was gated but unreachable; the feed path now populates
it, keeping the swarm first and the gate authoritative (#243).

Evidence: `docs/BETA_ACCEPTANCE.md` (acceptance matrix and run records),
`docs/BETA_DEPLOYMENT.md` (deployment steps), `ops/docs/library-dedup-runbook.md`
(repair procedures). Issue-level detail lives in Linear (`DJI-###`, project
*NetRunner Beta Readiness*).

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

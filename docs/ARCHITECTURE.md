# Djinn NETRUNNER — Architecture & Contracts

This document defines the runtime contracts and invariants for NETRUNNER, especially the worker/DB correctness model.

## Services
- **caddy**: Edge proxy and TLS termination.
- **SQLite (WAL)**: Primary system-of-record for jobs, logs, metadata, and concurrency primitives (PostgreSQL also supported).
- **ops-web (Go/Fiber)**: Management API + server-rendered templates + HTMX UI; WebSockets for console streaming (fanout filtered by job_id subscription, Phase 8).
- **ops-worker (Go)**: Background job orchestrator with native goroutine concurrency, heartbeats, and reaper.
- **slskd**: Acquisition daemon with bounded download slots.
- **gonic**: Streaming server (Subsonic-compatible).

## Core Services (Go)
The worker orchestrates multiple specialized services:
- **WatchlistService**: Manages automated discovery sources (Spotify, RSS, Last.fm, Discogs, local files).
- **ArtistTrackingService**: Monitors artists for new releases via MusicBrainz.
- **ReleaseMonitorService**: Periodic background task that checks monitored artists for new releases.
- **AcquisitionHandler**: Processes acquisition jobs - downloads files via slskd, enriches with metadata.
- **ScannerService**: Scans and indexes local music library, extracts metadata and AcoustID fingerprints.
- **MusicBrainzService**: Client for MusicBrainz API with caching.
- **AcoustIDService**: Audio fingerprinting lookup (Chromaprint/fpcalc) for metadata enrichment.
- **CacheService**: Persistent shadow cache for external API responses (MusicBrainz/Spotify/AcoustID).
- **NotificationService**: Webhook dispatcher for job completion events and quota warnings.
- **DiskQuotaService**: Calculates library disk usage and checks quota thresholds (Phase 8).
- **SlskdService**: Client wrapper for the slskd daemon (Soulseek download API).
- **SpotifyService**: Spotify client with background token refresh and caching.
- **DiscogsService**: Discogs API client for cover art, genre, and year enrichment.
- **GonicClient**: Subsonic API client for library streaming.
- **ProfileService**: Manages quality profile CRUD and defaults.

## Core Data Model
### Tables (minimum)
- **jobs**: Durable job records (state machine + execution metadata).
- **jobitems**: Durable units of work; MUST be created before execution (deterministic plan).
- **joblogs**: Append-only console lines for each job.
- **acquisitions**: Provenance and final-path record for imported items.
- **metadata_cache**: Persistent shadow cache for external API responses.
- **watchlists**: Automated discovery source configurations.
- **schedules**: Cron-based scheduling for watchlist syncs.
- **monitored_artists**: Artist tracking configuration.
- **tracks**: Indexed local library tracks.

## API Endpoints
### Watchlists
- `GET /api/watchlists` - List all watchlists
- `POST /api/watchlists` - Create new watchlist
- `DELETE /api/watchlists/:id` - Delete watchlist
- `POST /api/watchlists/:id/sync` - Trigger manual sync

### Artists (Monitoring)
- `GET /api/artists` - List monitored artists
- `POST /api/artists` - Add artist to monitoring
- `DELETE /api/artists/:id` - Remove artist monitoring

### Schedules
- `GET /api/schedules` - List all schedules
- `POST /api/schedules` - Create schedule
- `PATCH /api/schedules/:id` - Update schedule
- `DELETE /api/schedules/:id` - Delete schedule

### Dashboard
- `GET /` - Main dashboard (server-rendered)
- `GET /partials/stats` - Stats partial (HTMX)
- `GET /partials/watchlists` - Watchlists partial (HTMX)

## UI/UX Contract (Beta)
- Authenticated routes and HTMX partial endpoints rely on `AuthMiddleware`-populated `c.Locals("user")` for authorization context.
- The UI remains server-rendered + HTMX-first; client JS is limited to modal flow, console controls, CSRF headers, and responsive nav toggling.
- Console and operations pages must remain keyboard-navigable with visible focus states and clear loading/empty/error feedback.

### WebSocket
- `WS /ws/jobs/:job_id` - Per-job log streaming (filtered to specific job)
- `WS /ws/events` - System-wide event stream (admin-only)

## Concurrency + Correctness Invariants
1. **Contention-Safe Claims**: Jobs and jobitems are claimed using atomic status updates (SQLite) or `FOR UPDATE SKIP LOCKED` (PostgreSQL) to prevent duplicate claims.
2. **Explicit Exclusivity**: Per-scope locks (file-based or DB-level advisory locks) prevent multiple workers from executing the same scope (e.g., syncing the same playlist) simultaneously.
3. **Deterministic Work Plans**: `jobitems` are created before execution; retries resume without re-deriving metadata.
4. **Fair Scheduling**: Round-robin task selection across active jobs to prevent starvation.
5. **Authoritative Heartbeats**: Running jobs update `heartbeat_at` frequently; the reaper uses this to detect and recover from worker crashes.

## Agentic Interface (MCP)
NetRunner implements an embedded **Model Context Protocol (MCP)** server at `backend/cmd/agent`. This allows AI agents to:
- **Probe System**: Check connectivity and resource health (`probe_system`).
- **Manage Watchlists**: Add, list, or trigger sync on automated discovery sources (`add_watchlist`, `list_watchlists`, `sync_watchlist`).
- **Monitor Pipeline**: View real-time job logs and statuses (`list_jobs`, `get_job_logs`).
- **Search Library**: Query the combined Gonic and local indices (`search_library`).
- **Manage Libraries**: Scan or add library paths (`scan_library`, `add_library`).
- **Manage Artists**: List monitored artists (`list_monitored_artists`).
- **Manage Jobs**: Cancel or retry jobs (`cancel_job`, `retry_job`).
- **Query System**: Get stats summaries, quality profiles, and configured libraries (`get_stats`, `list_quality_profiles`, `list_libraries`).

## Security Posture

### Session & Auth
- **Session tokens**: 128-bit cryptographically random (crypto/rand), hex-encoded, stored in DB with 7-day TTL.
- **Cookie security**: HttpOnly enabled, SameSite=Lax, Secure flag set dynamically based on protocol (HTTPS in prod, HTTP for local dev).
- **Password storage**: bcrypt hashing (no plaintext).
- **Auth rate limiting**: Fiber rate limiter on `/api/auth/login` and `/api/auth/register` (default: 10 req/min).

### CSRF Protection
- **Double-submit cookie pattern**: JS reads `csrf_` cookie and attaches it as `X-CSRF-Token` header on all HTMX and Fetch requests.
- CSRF cookie uses SameSite=Lax; CookieSecure not explicitly set (follows cookie middleware defaults — prod deployments behind TLS should verify this is applied).
- No server-rendered hidden CSRF input fields (forms rely entirely on JS header injection).

### XSS Prevention
- **Pongo2 templates**: All user-supplied values rendered in templates use the `| escape` filter explicitly.
- **WebSocket log streaming**: Uses `html.EscapeString()` before broadcasting to clients.
- **Scope**: Track metadata (title, artist, album, genre, composer, cover URLs), artist names, watchlist names, library names, profile names, schedule names, search strings, file paths, SourceType, and all aria-label/hx-confirm attributes that include user data are escaped.
- **Not auto-escaped by default**: Pongo2 engine has no global autoescape — escaping is applied per-variable. Any new template variable rendering user data must use `| escape`.

### SQL Injection Prevention
- All database queries use **GORM parameterization** — no raw SQL string concatenation in production code paths.
- User-supplied filter/sort values are validated against whitelists before use in queries.

### File Path Traversal
- Library paths validated with `filepath.IsAbs()` + `filepath.Clean()` before use.
- Scanner uses `filepath.WalkDir` under the validated library root — no symlink escalation risk from the validated path boundary.

### SSRF / Outbound HTTP
- **Unified safe HTTP transport** (`safe_http.go`): All outbound HTTP clients (Spotify, MusicBrainz, Discogs, slskd, gonic, navidrome, cover art fetches) use a shared `http.Transport` with:
  - DNS rebinding protection (IP resolution before dial)
  - Private IP range blocking (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, ::1)
- `docker-compose.yml` no longer has `extra_hosts: ["host.docker.internal:host-gateway"]` (removed in Cycle 5).

### OOM Prevention
- Cover art responses limited to **10MB** via `io.LimitReader`.
- File size limits on upload/scan paths where applicable.

### Credential Leakage
- Subsonic clients (gonic, navidrome) use **token-based auth** (`t` + `s` params) instead of password in query string (`p` param).
- No credentials in URLs, logs, or error messages returned to clients.

### Vulnerability Scanning
- `govulncheck ./...` reports **no reachable vulnerabilities**.

### Dependency Security
- Go module dependencies vetted via `go mod verify` and periodic `govulncheck` runs.
- No known-CVE dependencies in the dependency tree (as of last audit).

## DB Connection Model
The system uses a unified GORM connection with specific optimizations for SQLite:
- **WAL Mode**: Enabled for high-concurrency read/write operations.
- **Busy Timeout**: Configured to 5000ms to prevent locking issues.
- **Synchronous**: Set to `NORMAL` for performance while maintaining crash-safety.


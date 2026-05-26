# NetRunner — Status & Roadmap

## Current Status: v0.0.1 — All Cycles Complete

Twelve cycles of development have shipped (Cycle A through Cycle 9), covering foundation, security, stability, capability, and polish.

### Completed (Cycles A–9)

#### Core Platform
- [x] Go backend (Fiber + GORM + SQLite/PostgreSQL)
- [x] Music acquisition pipeline (Soulseek via slskd)
- [x] Metadata enrichment (MusicBrainz, AcoustID, Discogs, Last.fm, ListenBrainz)
- [x] Library scanning, indexing, and pruning
- [x] Quality profiles with bitrate/speed/format preferences
- [x] Operations UI (HTMX + Pongo2 + cyberpunk theme)
- [x] WebSocket console with per-job log streaming
- [x] MCP server for AI agent interaction (20 tools)
- [x] CLI management tool (Cobra)

#### Watchlist Providers
- [x] Spotify (two-pronged: sp_dc cookie + OAuth, GraphQL Partner API)
- [x] Last.fm (paginated, validated)
- [x] ListenBrainz (paginated, validated)
- [x] Discogs (paginated, validated)
- [x] RSS/Bandcamp (proxy-aware, channel-title fallback)
- [x] Local files and directories
- [x] Lidarr (wired and config fields added)

#### Worker & Orchestration
- [x] Round-robin job dispatch with heartbeat monitoring
- [x] Panic recovery with structured error handling
- [x] Graceful shutdown with drain timeout
- [x] Zombie job recovery (stale running jobs re-queued)
- [x] Job requeue on lock failure (immediate, not waiting for zombie recovery)
- [x] Worker struct field sync (State, StartedAt, WorkerID, HeartbeatAt)
- [x] LyricsService wired into post-import enrichment
- [x] TranscoderService wired into post-import enrichment
- [x] YtdlpService as Soulseek fallback
- [x] NavidromeClient as GonicClient alternative (SubsonicClient interface)
- [x] Album-mode acquisition (peer directory browse, track discovery)
- [x] Gonic library refresh after import (WaitGroup-tracked)
- [x] Gonic→Navidrome scan fallback on error
- [x] Schedule-driven sync with cron parsing
- [x] Release monitor background task
- [x] Recording deduplication via AcoustID/MusicBrainz

#### Security
- [x] Session-cookie auth with RBAC (user/admin)
- [x] BOLA enforcement across all handlers
- [x] WebSocket ownership validation
- [x] HTTPOnly/Secure/SameSite cookies
- [x] CSP headers (script-src 'self', no inline scripts)
- [x] Proxy-aware HTTP client factory
- [x] Auth rate limiting
- [x] XSS protection in templates

#### Infrastructure
- [x] Docker Compose stack (PostgreSQL 16 + slskd + Gonic + Caddy)
- [x] SQLite WAL for local dev
- [x] LiteFS primary-node detection
- [x] LiteFS write-forwarding HTTP middleware
- [x] PostgreSQL advisory locks with concurrent test coverage
- [x] GORM auto-migrations
- [x] Prometheus metrics (server :8080, worker :9090)
- [x] SMTP email notifications (decoupled from webhook)
- [x] Webhook notifications

#### UI & Accessibility
- [x] Mobile nav toggle with hamburger icon
- [x] Keyboard navigation (skip-link, focus-visible, modal focus trap)
- [x] Role-based dashboard views (admin sees "All Users" scope)
- [x] Watchlist form with all 11 source types
- [x] Spotify sp_dc cookie linking UI
- [x] Modal container centralization

#### Documentation & DX
- [x] AGENTS.md with full repo map, API reference, MCP tool schemas
- [x] Ops runbooks (backup, upgrade, DR, SQLite→Postgres migration)
- [x] Database tier guidance (SQLite vs PostgreSQL)
- [x] Watchlist provider reference guide
- [x] Grafana dashboard documentation
- [x] ADR 0001: Multi-node SQLite with LiteFS
- [x] ADR 0002: Config-as-code evaluation (rejected — env vars stay)

#### Testing & CI
- [x] Unit tests across all packages
- [x] Integration test CI workflow (Postgres + slskd)
- [x] Performance benchmarks (job selection, item claim, metadata extraction)
- [x] Beta acceptance evidence matrix
- [x] go vet + govulncheck in CI
- [x] Docker build to ghcr.io

## Known Gaps & Future Work

### Post-v0.0.1

- [ ] E2E automated acceptance tests (library scan, artist CRUD, schedule CRUD, webhook smoke, quota warning)
- [ ] Full browser-verified mobile nav and keyboard accessibility
- [ ] Multi-environment config file support (if deployment complexity warrants)
- [ ] Horizontal worker scaling via LiteFS write forwarding
- [ ] Admin panel with user management UI
- [ ] Grafana dashboard JSON export in repo

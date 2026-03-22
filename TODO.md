# NetRunner — Status & Roadmap

## Current Status: Phase 8 Complete ✅

All Phase 8 items implemented: integration tests, cover art quality, MCP expansion, disk quota, AcoustID fingerprint storage.

### Completed
- [x] Go backend (Fiber + GORM + SQLite/PostgreSQL)
- [x] Music acquisition pipeline (Soulseek via slskd)
- [x] Metadata enrichment (MusicBrainz, AcoustID)
- [x] Library scanning & indexing
- [x] Watchlist management (Spotify, RSS, Last.fm, Discogs, local files)
- [x] Artist tracking & release monitoring
- [x] Quality profiles with bitrate/speed/format preferences
- [x] Operations UI (HTMX + Fiber + cyberpunk theme)
- [x] WebSocket console with per-job log streaming
- [x] MCP server for AI agent interaction
- [x] Background Spotify token refresh
- [x] Job completion webhook notifications
- [x] WebSocket auth & job ownership validation
- [x] Security hardening (XSS protection, auth middleware)
- [x] Cover art fallback chain (MusicBrainz → Discogs) with OGG/M4A support
- [x] Watchlist preview endpoint with HTML rendering
- [x] MCP tool expansion: sync_watchlist, get_stats, list_quality_profiles, list_libraries
- [x] Webhook notification schema documented
- [x] Integration tests: slskd, DiscogsService, SpotifyProvider, NotificationService, webhooks
- [x] Cover art quality: configurable source priority, MIME detection, image caching
- [x] Watchlist preview UI: track counts and source badges
- [x] MCP tool expansion: scan_library, add_library, list_monitored_artists, cancel_job, retry_job
- [x] Disk quota monitoring and alerts
- [x] AcoustID fingerprint storage in Track model

## Known Gaps & Future Work

### Medium Priority
- [ ] Notifications: add email support (SMTP integration)

### Low Priority / Nice-to-Have
- [ ] Multi-user UI: role-based dashboard views

# NetRunner — Status & Roadmap

## Current Status: Phase 7 Complete ✅

All core systems are implemented and merged. Post-Phase 7 hardening tasks are complete.

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

## Known Gaps & Future Work

### High Priority
- [ ] Cover art quality: improve fallback sources and embedding reliability
- [ ] Watchlist preview UI: improve rendering with track counts and source badges

### Medium Priority
- [ ] Notifications: add email support (SMTP integration)
- [ ] MCP agent tools: add scan_library, list_monitored_artists, cancel_job, retry_job
- [ ] Integration tests: slskd, watchlist providers, notification webhook

### Low Priority / Nice-to-Have
- [ ] Multi-user UI: role-based dashboard views
- [ ] Disk quota monitoring and alerts
- [ ] Acoustic fingerprinting (AcoustID fpcalc integration)

# NetRunner — Status & Roadmap

## Current Status: Phase 7 Complete ✅

All core systems are implemented and merged.

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

## Known Gaps & Future Work

### High Priority
- [ ] Cover art quality: improve fallback sources and embedding reliability
- [x] WebSocket filtering: per-job broadcast fanout with thread-safe subscription management

### Medium Priority
- [ ] Watchlist preview UI: button added, preview rendering needs polish
- [ ] Notifications: document webhook payload schema, add email support
- [ ] MCP agent tools: expand tool surface for richer agent interactions

### Low Priority / Nice-to-Have
- [ ] Test coverage: integration tests for slskd, watchlist providers
- [ ] Multi-user UI: role-based dashboard views
- [ ] Disk quota monitoring and alerts

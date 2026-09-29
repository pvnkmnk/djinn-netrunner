# Implementation History

> **Dated files in this directory are point-in-time records, not instructions.** The
> `YYYY-MM-DD-*.md` plans, gap lists and release checklists are kept as written: their
> unticked boxes, file names and commands describe the state on the date in the file
> name. Current guidance is `docs/DEPLOYMENT.md` for the dev and release paths,
> `docs/RUNBOOK.md` for operations, and `README.md` / `QUICKSTART.md` for a first setup.

## Phase 1-3: Foundation (PRs #22-24)
- Go backend initialization
- Database models and migrations
- MusicBrainz, slskd, Gonic service skeletons
- Worker orchestrator with job claiming

## Phase 4: Library Scanner (PRs #23-24)
- ScannerService for tag extraction
- Metadata enrichment pipeline
- Acoustic fingerprinting via AcoustID

## Phase 5: Quality Profiles (PR #25)
- CRUD API for quality profiles
- Bitrate/format/priority preferences
- Profile assignment per watchlist

## Phase 6: UI Implementation (PR #26)
- Full management UI with HTMX + Fiber
- Dashboard, watchlists, artists, schedules, libraries, profiles, jobs
- Cyberpunk glassmorphic theme
- WebSocket console with per-job streaming

## Phase 7: Hardening & Polish (PR #32)
- slskd health check in MCP system status
- Ambiguous artist search logging
- WebSocket per-job broadcast filtering
- Background Spotify token refresh
- Job completion webhook notifications
- MusicBrainz cover art fallback
- Watchlist preview endpoint
- Integration test expansion

See Git history for individual commit details: `git log --oneline --grep="phase\|feat\|fix" | head -50`

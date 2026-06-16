# NetRunner — Feature Overview

## Core Pipeline

### Watchlist Sources
Watchlists are the primary acquisition trigger. Each watchlist specifies a source type and URI:
- **Spotify**: Public/private playlists, Liked Songs, Discover Weekly via two-pronged auth (sp_dc cookie + OAuth)
- **Last.fm**: Loved tracks and top tracks (paginated, validated)
- **ListenBrainz**: User listen history
- **Discogs**: User wantlists (paginated, validated)
- **RSS/Atom**: Standard feed URLs including Bandcamp artist feeds
- **Lidarr**: Wanted album import
- **Local Files**: M3U, CSV, TXT playlists mounted into the container
- **Local Directories**: Watch a directory for new files

### Acquisition Engine
- Soulseek download via slskd API with peer reputation scoring
- Multi-variable quality scoring: bitrate, upload speed, queue depth
- MD5-based deduplication (never re-download the same file)
- Album-mode acquisition (peer directory browse, track discovery)
- YtdlpService as Soulseek fallback for direct downloads
- AcoustID fingerprinting for accurate track identification
- Metadata enrichment via MusicBrainz (MBID lookup, artist credit normalization)
- Discogs cover art, genre, and year enrichment
- Lyrics fetching via LRCLIB
- Audio transcoding via TranscoderService

### Library Management
- Scanner extracts tags from MP3, FLAC, OGG, M4A, WAV
- Configurable quality profiles (preferred bitrate/formats/sources)
- Gonic Subsonic API integration for streaming
- NavidromeClient as alternative streaming backend (SubsonicClient interface)
- Gonic→Navidrome scan fallback on error
- Cover art embedding and local caching
- Track pruning with detailed per-file logging
- Disk quota tracking

### Job Orchestration
- Deterministic work plans: job items created before execution
- Round-robin fairness: multiple jobs advance simultaneously
- Advisory locks per scope: prevents duplicate sync/scan operations
- Heartbeat-driven crash recovery with zombie job re-queue
- Graceful shutdown with drain timeout
- Panic recovery with structured error handling
- Job requeue on lock failure (immediate, not waiting for zombie recovery)
- Schedule-driven sync with cron parsing
- Release monitor background task for artist tracking

## Operations UI

- HTMX + server-rendered templates (no SPA framework)
- Cyberpunk terminal aesthetic with glassmorphic cards
- Real-time console via WebSocket per-job log streaming
- Mobile-responsive nav with hamburger toggle and keyboard accessibility
- Role-based dashboard views (admin sees "All Users" scope, non-admin users are scoped to their own data)
- Modal-based CRUD for all entities (watchlists, artists, schedules, profiles)
- Quality profile editor with format/bitrate/priority preferences
- Library browse with searchable/sortable/paginated track table
- Job detail with attempt/error display, retry and cancel buttons

## Notifications

- Webhook notifications on job completion
- SMTP email notifications (decoupled from webhook, same NotificationService)
- Configurable webhook URL and enable/disable toggle

## Agent Interface

- MCP server at `backend/cmd/agent` (20 tools) for AI agent interaction
- CLI at `backend/cmd/cli` for manual management
- Tools: probe system status, manage watchlists/artists/libraries, trigger syncs/scans, query library, manage jobs (cancel/retry), webhook registration

## Configuration

Environment variables via `.env` (see `.env.example`):
- `DATABASE_URL` — SQLite or PostgreSQL connection
- `SLSKD_URL`, `SLSKD_API_KEY` — Soulseek daemon
- `SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET` — Spotify OAuth (optional; sp_dc cookie preferred)
- `LASTFM_API_KEY`, `LISTENBRAINZ_TOKEN`, `DISCOGS_TOKEN` — provider API keys
- `MUSICBRAINZ_USER_AGENT`, `MUSICBRAINZ_API_KEY` — MusicBrainz configuration
- `ACOUSTID_API_KEY` — audio fingerprinting
- `LIDARR_URL`, `LIDARR_API_KEY` — Lidarr wanted album import
- `GONIC_URL`, `GONIC_USER`, `GONIC_PASS` — Gonic/Subsonic streaming server
- `MUSIC_LIBRARY` — library root path
- `NOTIFICATION_WEBHOOK_URL`, `NOTIFICATION_ENABLED` — job completion webhooks
- `PROXY_URL` — SOCKS5/HTTP proxy for outbound traffic
- `JWT_SECRET` — session/auth crypto secret
- `PORT`, `DOMAIN`, `ENVIRONMENT` — server configuration

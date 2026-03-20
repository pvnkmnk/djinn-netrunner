# NetRunner — Feature Overview

## Core Pipeline

### Watchlist Sources
Watchlists are the primary acquisition trigger. Each watchlist specifies a source type and URI:
- **Spotify**: Public playlists via URI (`spotify:playlist:...`)
- **RSS/Atom**: Standard feed URLs for music blogs/podcasts
- **Last.fm**: Artist/library collections
- **Discogs**: User collections and wantlists
- **Local Files**: M3U, CSV, TXT playlists mounted into the container

### Acquisition Engine
- Soulseek download via slskd API
- Multi-variable quality scoring: bitrate, upload speed, queue depth
- MD5-based deduplication (never re-download the same file)
- AcoustID fingerprinting for accurate track identification
- Metadata enrichment via MusicBrainz (MBID lookup, artist credit normalization)

### Library Management
- Scanner extracts tags from MP3, FLAC, OGG, M4A, WAV
- Configurable quality profiles (preferred bitrate/formats/sources)
- Gonic Subsonic API integration for streaming
- Cover art embedding and local caching

### Job Orchestration
- Deterministic work plans: job items created before execution
- Round-robin fairness: multiple jobs advance simultaneously
- Advisory locks per scope: prevents duplicate sync/scan operations
- Heartbeat-driven crash recovery
- Reaper requeues stale jobs automatically

## Operations UI

- HTMX + server-rendered templates (no SPA framework)
- Cyberpunk terminal aesthetic with glassmorphic cards
- Real-time console via WebSocket per-job log streaming
- Modal-based CRUD for all entities (watchlists, artists, schedules, profiles)
- Quality profile editor with format/bitrate/priority preferences

## Agent Interface

- MCP server at `backend/cmd/agent` for AI agent interaction
- CLI at `backend/cmd/cli` for manual management
- Tools: probe system status, manage watchlists, trigger syncs, query library

## Configuration

Environment variables via `.env` (see `.env.example`):
- `DATABASE_URL` — SQLite or PostgreSQL connection
- `SLSKD_URL`, `SLSKD_API_KEY` — Soulseek daemon
- `SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET` — Spotify OAuth
- `MUSIC_LIBRARY` — library root path
- `NOTIFICATION_WEBHOOK_URL`, `NOTIFICATION_ENABLED` — job completion webhooks

# Repository Atlas: Djinn NETRUNNER

## Project Responsibility
Console-first operations UI for a music acquisition pipeline. Monitors watchlists (Spotify, Last.fm, ListenBrainz, RSS, local files), acquires music via Soulseek P2P, enriches metadata (MusicBrainz, Discogs, AcoustID), and manages libraries through a Gonic/Subsonic-compatible server.

## System Entry Points
- `backend/cmd/server/main.go` — HTTP server (Fiber + HTMX + WebSocket) on :8080
- `backend/cmd/worker/main.go` — Background job processor (round-robin, 5 concurrent jobs)
- `backend/cmd/agent/main.go` — MCP server over stdio for AI agent integration
- `backend/cmd/cli/main.go` — Cobra CLI for terminal/script access
- `docker-compose.yml` — Docker Compose orchestration (server, worker, PostgreSQL, Caddy)

## Architecture Constraints
- **Language**: Go 1.25+
- **Database**: SQLite (CGO-free via `modernc.org/sqlite`) in WAL mode, or PostgreSQL
- **Frontend**: HTMX + server-rendered Pongo2 templates + vanilla CSS (no SPA)
- **Concurrency**: Native goroutines, round-robin job dispatch, advisory locking
- **Privacy**: All P2P/API traffic supports SOCKS5/HTTP proxying

## Directory Map

| Directory | Responsibility | Detailed Map |
|-----------|---------------|--------------|
| `backend/cmd/agent/` | MCP server — 20+ tools over stdio for AI agent integration | [View Map](backend/cmd/agent/codemap.md) |
| `backend/cmd/cli/` | Cobra CLI — human/agent command-line access to all operations | [View Map](backend/cmd/cli/codemap.md) |
| `backend/cmd/server/` | HTTP server — Fiber + HTMX + WebSocket + Pongo2 templates | [View Map](backend/cmd/server/codemap.md) |
| `backend/cmd/worker/` | Job processor — round-robin dispatch, heartbeat, zombie recovery | [View Map](backend/cmd/worker/codemap.md) |
| `backend/internal/agent/` | Agent logic — transport-agnostic functions shared by MCP and CLI | [View Map](backend/internal/agent/codemap.md) |
| `backend/internal/api/` | HTTP handlers — REST API, HTMX pages/partials, WebSocket, OAuth | [View Map](backend/internal/api/codemap.md) |
| `backend/internal/api/templates/` | Pongo2 engine — Jinja2-compatible Fiber ViewEngine adapter | [View Map](backend/internal/api/templates/codemap.md) |
| `backend/internal/config/` | Configuration — env-based loader for all service connections | [View Map](backend/internal/config/codemap.md) |
| `backend/internal/database/` | Data layer — GORM models, migrations, advisory locks, LiteFS | [View Map](backend/internal/database/codemap.md) |
| `backend/internal/interfaces/` | Abstractions — SpotifyClientProvider, WatchlistProvider contracts | [View Map](backend/internal/interfaces/codemap.md) |
| `backend/internal/services/` | Core logic — watchlists, jobs, P2P, metadata, artist tracking | [View Map](backend/internal/services/codemap.md) |
| `ops/caddy/` | Reverse proxy — Caddy config for production HTTPS | [View Map](ops/caddy/codemap.md) |
| `ops/db/` | Database ops — PostgreSQL init scripts and SQL migrations | [View Map](ops/db/codemap.md) |
| `ops/web/` | Web assets root — static files + Pongo2 templates | [View Map](ops/web/codemap.md) |
| `ops/web/static/css/` | Stylesheet — single CSS file, cyberpunk terminal palette | [View Map](ops/web/static/css/codemap.md) |
| `ops/web/static/js/` | Client JS — minimal vanilla JS for modals, console, filters | [View Map](ops/web/static/js/codemap.md) |
| `ops/web/templates/` | HTML templates — layouts, pages, HTMX partials | [View Map](ops/web/templates/codemap.md) |

## Data Flow (High-Level)

```
Watchlist Sources (Spotify, LastFM, RSS, Files)
        │
        ▼
  WatchlistService ──► SyncHandler ──► JobItems (DB)
        │                                   │
        ▼                                   ▼
  ArtistTrackingService              AcquisitionHandler
        │                                   │
        ▼                                   ▼
  ReleaseMonitorService              slskd (P2P search/download)
                                           │
                                           ▼
                                    MetadataExtractor
                                           │
                                           ▼
                                    AcoustID (fingerprint)
                                           │
                                           ▼
                                    MusicBrainz (enrich)
                                           │
                                           ▼
                                    ScannerService → Library (Gonic)
```

## Key Invariants
- Job items are created before execution; retries resume, never re-derive
- Per-scope exclusivity via advisory locks (session-level)
- Logs are the primary progress visualization (no progress bars)
- All new core features must be exposed via both CLI and MCP server
- Single CSS file, no SPA frameworks, no external queues

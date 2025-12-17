# Djinn NETRUNNER

**Console-first, self-hosted music acquisition and streaming appliance**

A complete system for automated music discovery, download, organization, and streaming with Subsonic compatibility. Built with FastAPI, PostgreSQL, and HTMX for a terminal-aesthetic operations console.

![Status](https://img.shields.io/badge/status-production--ready-green)
![Docker](https://img.shields.io/badge/docker-compose-blue)
![License](https://img.shields.io/badge/license-MIT-blue)

## 🎯 What is NETRUNNER?

NETRUNNER is a self-hosted music pipeline that:
- 📥 **Acquires** music from Soulseek via slskd integration
- 🏷️ **Organizes** files with metadata extraction and smart library structure
- 🎵 **Streams** via Gonic (Subsonic-compatible server)
- 🖥️ **Monitors** through a live console with WebSocket streaming
- 🔄 **Recovers** from crashes with advisory locks and heartbeats

## ✨ Features

### Core Pipeline
- **Source Management**: File lists, Spotify playlists (extensible)
- **Intelligent Search**: Quality-based ranking (bitrate, speed, queue length)
- **Concurrent Downloads**: Configurable slot management
- **Metadata Extraction**: MP3, FLAC, M4A, OGG, Opus, WMA support
- **Library Organization**: `Artist/Album/Track - Title` structure
- **Automatic Indexing**: Gonic library refresh after imports

### Job Orchestration
- **Round-Robin Fairness**: Multiple jobs progress simultaneously
- **Crash-Safe**: Advisory locks + heartbeats + automatic reaper
- **Deterministic Work Plans**: Job items created before execution
- **Event-Driven**: PostgreSQL LISTEN/NOTIFY (no polling)
- **State Machines**: Explicit job and item state transitions

### Operations Console
- **Live Log Streaming**: WebSocket-based with attach modes
- **Source Management UI**: Modal-based CRUD with toast notifications
- **Console Controls**: Auto-scroll, filters (OK/INFO/ERR), copy, clear
- **Terminal Aesthetics**: Monospace fonts, dark colors, minimal JS
- **Job Monitoring**: Real-time stats and progress tracking

## 🚀 Quick Start

### Prerequisites
- Docker & Docker Compose
- Soulseek account credentials

### Installation

```bash
# Clone the repository
git clone https://github.com/pvnkmnk/djinn-netrunner.git
cd djinn-netrunner

# Configure environment
cp .env.example .env
# Edit .env with your Soulseek credentials

# Start the stack
docker compose up -d

# Access the console
open http://localhost:8000
# or https://localhost (Caddy with auto TLS)
```

### First Sync

**Option 1: Web UI**
1. Click **+ ADD** in the SOURCES section
2. Fill in: Display Name, Source Type (file_list), URI path
3. Click **SYNC** to start acquisition
4. Watch live console logs

**Option 2: Example Playlist**
```bash
# Copy example playlist
docker compose exec ops-worker mkdir -p /data/playlists
docker compose cp examples/playlist_example.txt ops-worker:/data/playlists/favorites.txt

# Add source via CLI
docker compose exec ops-worker python add_source.py \
  "postgresql://musicops:yourpassword@postgres:5432/musicops" \
  file_list \
  /data/playlists/favorites.txt \
  "Example Favorites"
```

See **[QUICKSTART.md](QUICKSTART.md)** for detailed setup instructions.

## 📚 Documentation

| Document | Description |
|----------|-------------|
| **[QUICKSTART.md](QUICKSTART.md)** | Setup guide and first run |
| **[FEATURES.md](FEATURES.md)** | Complete feature list and implementation status |
| **[SOURCE_MANAGEMENT_UI.md](SOURCE_MANAGEMENT_UI.md)** | Source management UI guide |
| **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** | System architecture and invariants |
| **[docs/UIIMPLEMENTATION.md](docs/UIIMPLEMENTATION.md)** | Console patterns and HTMX contracts |
| **[docs/RUNBOOK.md](docs/RUNBOOK.md)** | Operational procedures |
| **[docs/WHITEPAPER.md](docs/WHITEPAPER.md)** | Product intent and production posture |
| **[AGENTS.md](AGENTS.md)** | Development guidelines for AI assistants |

## 🏗️ Architecture

```
┌─────────┐      ┌──────────┐      ┌──────────────┐
│  Caddy  │─────▶│ ops-web  │─────▶│  PostgreSQL  │
│  (TLS)  │      │ (FastAPI)│      │ (jobs, logs) │
└─────────┘      └──────────┘      └──────────────┘
                       │                    ▲
                       │                    │
                       ▼                    │
                 ┌──────────┐               │
                 │ops-worker│───────────────┘
                 │(asyncio) │
                 └──────────┘
                       │
                       ├─────▶ slskd (acquisition)
                       └─────▶ Gonic (streaming)
```

### Service Stack

- **Caddy**: TLS termination, reverse proxy, HTTP/3 support
- **PostgreSQL 16**: State storage, advisory locks, LISTEN/NOTIFY
- **ops-web**: FastAPI + HTMX UI + WebSocket streaming
- **ops-worker**: Asyncio orchestrator with round-robin scheduler
- **slskd**: Soulseek daemon for music acquisition
- **Gonic**: Subsonic-compatible streaming server

## 🎨 Design Principles

1. **Console-First UX**: Logs are the primary progress visualization
2. **HTMX + Server-Rendered**: No SPA frameworks, minimal JavaScript
3. **PostgreSQL as Single Source of Truth**: No external queues/caches
4. **Event-Driven**: LISTEN/NOTIFY + WebSockets (never polling)
5. **Deterministic Work Plans**: Job items created before execution
6. **Crash-Safe**: Advisory locks, heartbeats, automatic recovery

## 🔧 Configuration

Key environment variables in `.env`:

```bash
# PostgreSQL
POSTGRES_PASSWORD=secure_password

# Soulseek credentials
SLSKD_USERNAME=your_username
SLSKD_PASSWORD=your_password
SLSKD_API_KEY=random_api_key

# Domain (localhost for dev)
DOMAIN=localhost
```

## 📊 Job Types

| Type | Description |
|------|-------------|
| **sync** | Parse playlist/source and create acquisition jobs |
| **acquisition** | Search slskd, download tracks, import to library |
| **index_refresh** | Trigger Gonic library scan |
| **import** | Metadata enrichment (extensible) |

## 🛠️ Development

### Project Structure

```
netrunner/
├── docker-compose.yml          # Service definitions
├── .env.example                # Configuration template
├── ops/
│   ├── db/init/                # PostgreSQL schema
│   ├── web/                    # FastAPI ops-web
│   │   ├── main.py
│   │   ├── source_manager.py
│   │   ├── templates/
│   │   └── static/
│   ├── worker/                 # Asyncio ops-worker
│   │   ├── main.py
│   │   ├── job_handlers.py
│   │   ├── slskd_client.py
│   │   ├── gonic_client.py
│   │   ├── import_pipeline.py
│   │   └── metadata_extractor.py
│   └── caddy/
│       └── Caddyfile
├── examples/
│   └── playlist_example.txt
└── docs/
```

### Cursor AI Integration

This project includes AI-ready documentation in `.cursor/rules/`:
- `00-project-overview.mdc` - High-level architecture
- `10-backend-worker.mdc` - Worker orchestration patterns
- `20-ui-console.mdc` - HTMX console implementation
- `30-sql-migrations.mdc` - Database schema guidelines

## 🎯 Roadmap

- [x] Core acquisition pipeline
- [x] Source management UI
- [x] WebSocket console streaming
- [x] Round-robin job scheduler
- [x] Metadata extraction and import
- [ ] Spotify playlist integration
- [ ] MusicBrainz metadata enrichment
- [ ] Cover art downloading
- [ ] Automatic scheduling (cron-like)
- [ ] Multi-user support with auth

## 🤝 Contributing

Contributions welcome! Please:
1. Read [AGENTS.md](AGENTS.md) for development guidelines
2. Keep changes aligned with console-first design principles
3. Update documentation when modifying behavior
4. Test with example playlists before submitting

## 📝 License

MIT License - see LICENSE file for details

## 🙏 Acknowledgments

- Built with [FastAPI](https://fastapi.tiangolo.com/)
- UI powered by [HTMX](https://htmx.org/)
- Music acquisition via [slskd](https://github.com/slskd/slskd)
- Streaming via [Gonic](https://github.com/sentriz/gonic)
- Proxy by [Caddy](https://caddyserver.com/)

---

**Status**: Production-ready | **Built with**: Claude Code | **Stack**: Python, PostgreSQL, Docker

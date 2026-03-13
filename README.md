# Djinn NETRUNNER 2.0

**Standalone, high-fidelity music acquisition and streaming appliance**

NetRunner is a modern, Go-native system for automated music discovery, download, organization, and streaming. Built for performance and privacy, it transforms your server into a resilient music acquisition terminal.

![Status](https://img.shields.io/badge/status-v2.1--curation--update-cyan)
![Architecture](https://img.shields.io/badge/architecture-standalone--sqlite-blue)
![UI](https://img.shields.io/badge/ui-htmx--cyberpunk-magenta)

---

## 🎯 What is NETRUNNER?

NetRunner 2.1 is a complete architectural evolution of the original music pipeline. It provides a "zero-config" standalone experience with a high-fidelity operations console and intelligent library curation.

- 📥 **Acquisition**: Seamless integration with Soulseek (via `slskd`).
- 🏗️ **Standalone Architecture**: Single-binary focus with CGO-free SQLite (WAL mode) or PostgreSQL.
- 🏷️ **Metadata Resilience**: Persistent "Shadow Cache" for MusicBrainz & Spotify.
- ⚡ **High-Performance**: Concurrent worker pools and round-robin task orchestration.
- 🛡️ **Privacy-First**: Native SOCKS5/HTTP proxy support for all P2P and API traffic.
- 🖥️ **Cyberpunk UI**: A modern, interactive Bento Grid dashboard powered by HTMX and Fiber.
- 🤖 **Agent-Native**: Built-in MCP (Model Context Protocol) server and CLI for autonomous management.

## ✨ Key Features

### The Console (UI/UX)
- **Bento Grid Layout**: Optimized, responsive dashboard for stats, jobs, and watchlists.
- **Real-time Log Streaming**: High-fidelity WebSocket console with regex syntax highlighting.
- **Fault Detection**: Automated "Jump to Error" navigation for rapid troubleshooting.
- **Glassmorphic Aesthetic**: Deep cyberpunk theme with high-quality typography (Orbitron/Inter).

### Core Pipeline
- **Unified Watchlists**: Single paradigm for all monitoring sources (Spotify, RSS, Local Files).
- **Intelligent Search**: Multi-variable quality ranking (bitrate, speed, queue depth).
- **Smart Deduplication**: MD5 hash-based verification ensures you never download or import the same file twice.
- **Enhanced Enrichment**: Automatic MusicBrainz integration to fetch recording/release IDs and high-res cover art.
- **Dynamic Library Routing**: Configurable library paths via the `MUSIC_LIBRARY` environment variable.
- **Parallel Scanning**: Concurrent IO worker pool for ultra-fast library imports.
- **Crash-Safe**: Robust heartbeat-driven recovery and automated zombie job cleanup.

---

## 🚀 Quick Start

### Prerequisites
- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- A [Soulseek](https://www.slsknet.org/) account (configured via `slskd`)

### 1. Clone & Configure
```bash
git clone https://github.com/pvnkmnk/netrunner.git
cd netrunner
cp .env.example .env
```

### 2. Environment Setup
Edit `.env` and provide your credentials:
```env
# slskd Soulseek credentials
SLSKD_USERNAME=your_username
SLSKD_PASSWORD=your_password
SLSKD_API_KEY=your_random_api_key

# Database (Defaults to SQLite if empty)
DATABASE_URL=netrunner.db

# Library Path
MUSIC_LIBRARY=/music

# AcoustID (Optional, for audio fingerprinting)
ACOUSTID_API_KEY=your_acoustid_api_key
```

### 3. Launch the Appliance
```bash
docker compose up -d
```
Access the management console at `http://localhost:8080`.

---

## 🛠️ Management CLI

NetRunner includes a powerful Go-native CLI for manual management and automation.

```bash
# Enter the API container
docker exec -it netrunner-api sh

# List all watchlists
netrunner-cli watchlist list

# Add a new Spotify watchlist
netrunner-cli watchlist add "My Playlist" "spotify_playlist" "https://open.spotify.com/playlist/..."

# Trigger a sync for a specific watchlist
netrunner-cli watchlist sync <watchlist-uuid>

# Check system status
netrunner-cli status
```

---

## 🏗️ Technical Architecture

NetRunner 2.0 uses a unified Go 1.25 backend:
- **NetRunner API (Fiber)**: High-performance web server handling the UI and real-time event fanout.
- **NetRunner Worker**: Multi-threaded orchestrator using native goroutines for discovery and acquisition.
- **SQLite (WAL)**: Default persistence layer providing ACID compliance with zero external dependencies.
- **MCP Server**: Embedded server at `backend/cmd/agent` for AI agent interaction.

### Directory Structure
```
├── backend/
│   ├── cmd/
│   │   ├── server/          # Web API entry point
│   │   ├── worker/          # Background worker entry point
│   │   ├── cli/             # Management CLI
│   │   └── agent/           # MCP Server for AI agents
│   └── internal/
│       ├── api/             # HTTP & WebSocket handlers
│       ├── database/        # GORM models and connection logic
│       └── services/        # Business logic (Spotify, slskd, Gonic)
├── ops/
│   ├── web/
│   │   ├── static/          # CSS/JS assets
│   │   └── templates/       # HTMX templates
│   └── caddy/               # Reverse proxy config
└── conductor/               # Track management and development docs
```

---

## 🔧 Core Design Decisions

1. **Native Concurrency**: Leverages Go's scheduler for fair, round-robin job processing across multiple sources.
2. **Metadata Caching**: Dramatic reduction in external API calls (MusicBrainz/Spotify) via a persistent cache layer.
3. **HTMX + Fiber**: A "no-build" frontend approach that delivers a rich, SPA-like experience with minimal complexity.
4. **Agentic Surface**: First-class support for AI agents through a comprehensive CLI and MCP tools.

---

## 🤝 Contributing

We welcome contributions that align with our "Console-First" and "Standalone" design principles. 

**For AI Agents**: Please read [AGENTS.md](./AGENTS.md) for architectural invariants and development constraints before proposing changes.

## 📝 License

MIT License - see [LICENSE](LICENSE) for details.

---
**Build status**: Finalized | **Architecture**: Go 1.25, SQLite, Fiber, HTMX | **Built with**: Claude Code

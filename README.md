# Djinn NETRUNNER 2.0

**Standalone, high-fidelity music acquisition and streaming appliance**

NetRunner is a modern, Go-native system for automated music discovery, download, organization, and streaming. Built for performance and privacy, it transforms your server into a resilient music acquisition terminal.

![Status](https://img.shields.io/badge/status-v0.0.1--release-green)
![Architecture](https://img.shields.io/badge/architecture-standalone--sqlite-blue)
![UI](https://img.shields.io/badge/ui-htmx--cyberpunk-magenta)
![Security](https://img.shields.io/badge/security-session--auth-brightgreen)

---

## 🎯 What is NETRUNNER?

NetRunner is a security-hardened, performance-optimized music acquisition pipeline. It provides a "zero-config" standalone experience with a high-fidelity operations console, intelligent library curation, and comprehensive multi-user data isolation.

- 📥 **Acquisition**: Seamless integration with Soulseek (via `slskd`).
- 🏗️ **Standalone Architecture**: Single-binary focus with CGO-free SQLite (WAL mode) or PostgreSQL.
- 🏷️ **Metadata Resilience**: Persistent "Shadow Cache" for MusicBrainz & Spotify.
- ⚡ **High-Performance**: Concurrent worker pools and round-robin task orchestration.
- 🛡️ **Privacy-First**: Native SOCKS5/HTTP proxy support for all P2P and API traffic.
- 🛡️ **Multi-User Security**: Broken Object Level Authorization (BOLA) protection across all endpoints with per-user data isolation.
- 🖥️ **Cyberpunk UI**: A modern, interactive Bento Grid dashboard powered by HTMX and Fiber.
- 🤖 **Agent-Native**: Built-in MCP (Model Context Protocol) server and CLI for autonomous management.
- ⚡ **Optimized Performance**: Eliminated redundant database lookups, consolidated stats queries, and improved HTMX feedback.
- ♿ **Accessible Console**: ARIA labels, contextual confirmations, and screen-reader-friendly management interfaces.

## ✨ Key Features

### The Console (UI/UX)
- **Bento Grid Layout**: Optimized, responsive dashboard for stats, jobs, and watchlists.
- **Real-time Log Streaming**: High-fidelity WebSocket console with regex syntax highlighting.
- **Fault Detection**: Automated "Jump to Error" navigation for rapid troubleshooting.
- **Glassmorphic Aesthetic**: Deep cyberpunk theme with high-quality typography (Orbitron/Inter).

### Core Pipeline
- **Unified Watchlists**: Single paradigm for all monitoring sources (Spotify, RSS, Last.fm, Discogs, local files).
- **Artist Tracking**: Monitor specific artists for new releases via MusicBrainz integration.
- **Scheduled Syncing**: Cron-based scheduling for automated watchlist and artist monitoring.
- **Intelligent Search**: Multi-variable quality ranking (bitrate, speed, queue depth).
- **Smart Deduplication**: MD5 hash-based verification ensures you never download or import the same file twice.
- **Enhanced Enrichment**: Automatic MusicBrainz and AcoustID integration for recording IDs and metadata.
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
git clone https://github.com/pvnkmnk/djinn-netrunner.git
cd djinn-netrunner
cp .env.example .env
```

### 2. Environment Setup
Edit `.env` and provide your credentials:
```env
# slskd Soulseek credentials
SLSKD_USERNAME=your_username
SLSKD_PASSWORD=your_password
SLSKD_API_KEY=your_random_api_key

# Database
POSTGRES_PASSWORD=your_secure_password
DATABASE_URL=postgresql://musicops:your_secure_password@postgres:5432/musicops?sslmode=disable

# Session/auth secret
JWT_SECRET=replace_with_a_long_random_secret

# Library Path
MUSIC_LIBRARY=/music

# AcoustID (Optional, for audio fingerprinting)
ACOUSTID_API_KEY=your_acoustid_api_key
```

### 3. Launch the Appliance
```bash
docker compose up -d --build
```
Access the management console at `http://localhost`.

---

## 🛠️ Management CLI

NetRunner includes a Go-native CLI for manual management and automation.

```bash
# Run from source checkout
cd backend

# List all watchlists
go run ./cmd/cli watchlist list

# Add a new Spotify watchlist
go run ./cmd/cli watchlist add "My Playlist" "spotify_playlist" "spotify:playlist:..."

# Trigger a sync for a specific watchlist
go run ./cmd/cli watchlist sync <watchlist-uuid>

# Check system status
go run ./cmd/cli status
```

For full CLI command coverage, run:
```bash
cd backend
go run ./cmd/cli --help
```

### Validation
Use the repo validation scripts before submitting changes:
```bash
# PowerShell
pwsh -File scripts/validate.ps1

# Bash
bash scripts/validate.sh
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
│       ├── agent/           # MCP agent tools and handlers
│       ├── api/             # HTTP & WebSocket handlers
│       ├── config/          # Configuration loading
│       ├── database/        # GORM models and connection logic
│       ├── interfaces/      # Interface definitions
│       └── services/        # Business logic (Spotify, slskd, Gonic)
├── ops/
│   ├── web/
│   │   ├── static/              # CSS/JS assets
│   │   └── templates/           # HTMX templates
│   └── caddy/                   # Reverse proxy config

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
**Architecture**: Go 1.25, SQLite, Fiber, HTMX

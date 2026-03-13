# Djinn NETRUNNER 2.0

**Standalone, high-fidelity music acquisition and streaming appliance**

NetRunner is a modern, Go-native system for automated music discovery, download, organization, and streaming. Built for performance and privacy, it transforms your server into a resilient music acquisition terminal.

![Status](https://img.shields.io/badge/status-v2.0--go--native-cyan)
![Architecture](https://img.shields.io/badge/architecture-standalone--sqlite-blue)
![UI](https://img.shields.io/badge/ui-htmx--cyberpunk-magenta)

---

## 🎯 What is NETRUNNER?

NetRunner 2.0 is a complete architectural evolution of the original music pipeline. It provides a "zero-config" standalone experience with a high-fidelity operations console.

- 📥 **Acquisition**: Seamless integration with Soulseek (via `slskd`).
- 🏗️ **Standalone Architecture**: Single-binary focus with CGO-free SQLite (WAL mode) or PostgreSQL.
- 🏷️ **Metadata Resilience**: Persistent "Shadow Cache" for MusicBrainz & Spotify.
- ⚡ **High-Performance**: Concurrent worker pools and round-robin task orchestration.
- 🛡️ **Privacy-First**: Native SOCKS5/HTTP proxy support for all P2P and API traffic.
- 🖥️ **Cyberpunk UI**: A modern, interactive Bento Grid dashboard powered by HTMX and Fiber.
- 🤖 **Agent-Native**: Built-in MCP (Model Context Protocol) server for autonomous management by AI agents.

## ✨ Key Features

### The Console (UI/UX)
- **Bento Grid Layout**: Optimized, responsive dashboard for stats, jobs, and sources.
- **Real-time Log Streaming**: High-fidelity WebSocket console with regex syntax highlighting.
- **Fault Detection**: Automated "Jump to Error" navigation for rapid troubleshooting.
- **Glassmorphic Aesthetic**: Deep cyberpunk theme with high-quality typography (Orbitron/Inter).

### Core Pipeline
- **Watchlist Management**: Support for local file registries and Spotify playlist uplinks.
- **Intelligent Search**: Multi-variable quality ranking (bitrate, speed, queue depth).
- **Parallel Scanning**: Concurrent IO worker pool for ultra-fast library imports.
- **Crash-Safe**: Robust heartbeat-driven recovery and persistent job states.

---

## 🚀 Quick Start

### Prerequisites
- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- A [Soulseek](https://www.slsknet.org/) account (configured via `slskd`)
- (Optional) Spotify API credentials for playlist syncing

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
```

### 3. Launch the Appliance
```bash
docker compose up -d
```
Access the management console at `http://localhost:8000`.

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

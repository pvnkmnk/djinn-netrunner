# Djinn NETRUNNER

**Standalone, high-fidelity music acquisition and streaming appliance**

NetRunner is a modern, Go-native system for automated music discovery, download, organization, and streaming. Built for performance and privacy, it transforms your server into a resilient music acquisition terminal.

![Status](https://img.shields.io/badge/status-v2.0--go--native-cyan)
![Architecture](https://img.shields.io/badge/architecture-standalone--sqlite-blue)
![UI](https://img.shields.io/badge/ui-htmx--cyberpunk-magenta)

## 🎯 What is NETRUNNER?

NetRunner 2.0 is a complete architectural evolution of the original music pipeline. It provides a "zero-config" standalone experience with a high-fidelity operations console.

- 📥 **Acquisition**: Seamless integration with Soulseek (via slskd).
- 🏗️ **Standalone Architecture**: Single-binary focus with CGO-free SQLite (WAL mode).
- 🏷️ **Metadata Resilience**: Persistent "Shadow Cache" for MusicBrainz & Spotify.
- ⚡ **High-Performance**: Concurrent worker pools and round-robin task orchestration.
- 🛡️ **Privacy-First**: Native SOCKS5/HTTP proxy support for all P2P traffic.
- 🖥️ **Cyberpunk UI**: A modern, interactive Bento Grid dashboard powered by HTMX.

## ✨ Key Features

### The Console (UI/UX)
- **Bento Grid Layout**: Optimized, responsive dashboard for stats, jobs, and sources.
- **Real-time Log Streaming**: High-fidelity WebSocket console with regex syntax highlighting.
- **Fault Detection**: Automated "Jump to Error" navigation for rapid troubleshooting.
- **Glassmorphic Aesthetic**: Deep cyberpunk theme with high-quality typography (Orbitron/Inter).

### Core Pipeline
- **Source Management**: Support for local file registries and Spotify playlist uplinks.
- **Intelligent Search**: Multi-variable quality ranking (bitrate, speed, queue depth).
- **Parallel Scanning**: Concurrent IO worker pool for ultra-fast library imports.
- **Crash-Safe**: Robust locking mechanisms and heartbeat-driven recovery.

## 🚀 Quick Start

### Installation (Docker)

```bash
# Clone the repository
git clone https://github.com/pvnkmnk/netrunner.git
cd netrunner

# Configure environment
cp .env.example .env
# Add your SLSKD_API_KEY and SLSKD_URL

# Launch the appliance
docker compose up -d
```

Access the terminal at `http://localhost:8000` (or `https://your-domain` via Caddy).

## 🏗️ Technical Architecture

NetRunner 2.0 uses a unified Go backend:
- **NetRunner API (Fiber)**: Handles the management console and real-time event fanout.
- **NetRunner Worker**: Orchestrates the acquisition pipeline using native goroutines.
- **SQLite (WAL)**: Provides a durable, zero-dependency persistence layer.

## 🔧 Core Design Decisions

1. **Native Concurrency**: Leverages Go's scheduler for fair job processing.
2. **Metadata Caching**: Dramatically reduces external API dependency and latency.
3. **Database-Agnostic Modeling**: GORM-based schema that works seamlessly on SQLite and PostgreSQL.
4. **Minimalist Frontend**: Pure HTML/CSS + HTMX for a fast, responsive interface with no JS framework bloat.

## 🤝 Contributing

We welcome contributions that align with our "Console-First" and "Standalone" design principles. Please see `AGENTS.md` for AI-assisted development guidelines.

## 📝 License

MIT License - see [LICENSE](LICENSE) for details.

---
**Build status**: Finalized | **Architecture**: Go 1.25, SQLite, Fiber, HTMX | **Built with**: Claude Code

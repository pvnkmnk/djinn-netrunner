# Djinn NETRUNNER 2.0

**Standalone, high-fidelity music acquisition and streaming appliance**

NetRunner is a modern, Go-native system for automated music discovery, download, organization, and streaming. Built for performance and privacy, it transforms your server into a resilient music acquisition terminal.

![Status](https://img.shields.io/badge/status-v0.0.1--release-green)
![Architecture](https://img.shields.io/badge/architecture-postgresql--sqlite-blue)
![UI](https://img.shields.io/badge/ui-htmx--cyberpunk-magenta)
![Security](https://img.shields.io/badge/security-session--auth-brightgreen)
[![CI](https://github.com/pvnkmnk/djinn-netrunner/actions/workflows/ci.yml/badge.svg)](https://github.com/pvnkmnk/djinn-netrunner/actions/workflows/ci.yml)
[![Integration Tests](https://github.com/pvnkmnk/djinn-netrunner/actions/workflows/integration.yml/badge.svg)](https://github.com/pvnkmnk/djinn-netrunner/actions/workflows/integration.yml)

---

## 🎯 What is NETRUNNER?

NetRunner is a security-hardened, performance-optimized music acquisition pipeline. It provides a "zero-config" standalone experience with a high-fidelity operations console, intelligent library curation, and comprehensive multi-user data isolation.

- 📥 **Acquisition**: Seamless integration with Soulseek (via `slskd`).
- 🏗️ **Two editions, one codebase**: **Djinn-Netrunner** runs on PostgreSQL (advisory locks, `LISTEN/NOTIFY`, concurrent workers); **NetrunnerLite** runs on a single CGO-free SQLite (WAL) file.
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
- **Unified Watchlists**: Single paradigm for all monitoring sources (Spotify playlists/liked/discover, Last.fm loved/top, ListenBrainz, Discogs, Lidarr, RSS, local files).
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

Every variable the project reads, what form each one takes, and the exact file it
belongs in (including the CI, Linear and Cloudflare credentials this list does not
cover) is in [docs/SECRETS_MANIFEST.md](docs/SECRETS_MANIFEST.md). A local process
can also take its environment from Infisical instead of `.env` — see
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#supplying-secrets-from-infisical-optional).

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
- **PostgreSQL**: The development and verification target (ADR 0004), and the database Djinn-Netrunner ships on. **SQLite (WAL)**: The zero-dependency alternative NetrunnerLite runs on — ACID-compliant with no external service, single-worker.
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

## 💾 Database Support

NetRunner ships in two editions over one codebase, and **PostgreSQL is what the project itself develops and verifies against** ([ADR 0004](docs/decisions/0004-postgres-first-development-and-editions.md)):

| Edition / deployment | Database | Notes |
|---|---|---|
| **Djinn-Netrunner** (dev and production) | PostgreSQL 16 | Advisory locks, `LISTEN/NOTIFY` wakeup, concurrent workers; the dev stack runs it on a loopback publish |
| **NetrunnerLite** (single user, zero config) | SQLite WAL | One worker, polling wakeups, no external service to run |
| Multi-node / horizontal scaling | PostgreSQL | Multiple workers across hosts |
| LiteFS + SQLite cluster | — | Not the recommended path; [ADR 0001](docs/decisions/0001-multi-node-sqlite-litefs.md) stays on record |

**Key differences:**
- **Advisory locks**: Postgres uses `pg_try_advisory_lock` for real session-level mutual exclusion; SQLite uses a `TableLockManager` (row-based emulation — **single-worker only**)
- **Job wakeup**: Postgres supports `LISTEN/NOTIFY` for instant worker notification; SQLite workers poll on interval
- **Concurrent workers**: Multiple worker instances require Postgres for safe concurrent job claims
- **`LiteFSGuard`**: Automatically detects LiteFS primary node and adjusts worker behavior

> The startup warning stands: SQLite with `MaxConcurrentJobs > 1` is NetrunnerLite hitting its single-worker limit, not a supported production configuration. Use PostgreSQL for concurrent workloads.

For operational runbooks (backup, upgrade, migration), see [`docs/RUNBOOK.md`](docs/RUNBOOK.md).

---

## 🔧 Core Design Decisions

1. **Native Concurrency**: Leverages Go's scheduler for fair, round-robin job processing across multiple sources.
2. **Metadata Caching**: Dramatic reduction in external API calls (MusicBrainz/Spotify) via a persistent cache layer.
3. **HTMX + Fiber**: A "no-build" frontend approach that delivers a rich, SPA-like experience with minimal complexity.
4. **Agentic Surface**: First-class support for AI agents through a comprehensive CLI and MCP tools.

---

## 🔒 Privacy & Proxy

All outbound HTTP traffic from provider API clients is routed through a centralized proxy-aware client factory (`NewProxyAwareHTTPClient`). Set the `PROXY_URL` environment variable to route traffic through an HTTP or SOCKS5 proxy.

**Proxied** (when `PROXY_URL` is set):
- MusicBrainz, AcoustID, Discogs, Last.fm, ListenBrainz, Lidarr, LRCLIB lyrics
- Gonic/Navidrome/Plex/Jellyfin library server API calls
- slskd API calls and webhook notifications

**Not proxied by NetRunner** (handle at the network/VPN layer):
- Spotify OAuth transport (uses `oauth2.Config.Client()` which manages its own HTTP transport)
- slskd Soulseek P2P traffic (managed by slskd's own network config)

```bash
# Example: route all provider traffic through a SOCKS5 proxy
PROXY_URL=socks5://127.0.0.1:1080

# Example: HTTP proxy
PROXY_URL=http://proxy.example.com:8080
```

---

## 📊 Observability

NetRunner exposes `/metrics` endpoints in Prometheus exposition format. The server exposes metrics on `:8080/metrics` and the worker on `:9090/metrics`. Scraped metrics include:

| Metric | Type | Labels | Description |
|---|---|---|---|
| `netrunner_worker_jobs_running` | gauge | — | Active jobs in the worker |
| `netrunner_worker_jobs_queued` | gauge | — | Jobs waiting to be claimed |
| `netrunner_worker_jobs_processed_total` | counter | `type`, `outcome` | Jobs completed (succeeded/failed) |
| `netrunner_worker_job_duration_seconds` | histogram | `type` | Processing time per job |
| `netrunner_worker_items_processed_total` | counter | `outcome` | Job items processed |
| `netrunner_worker_zombie_jobs_recovered_total` | counter | — | Stale jobs reset by zombie recovery |
| `netrunner_external_api_calls_total` | counter | `service`, `status` | External API call counts |
| `netrunner_external_api_duration_seconds` | histogram | `service` | External API latency |
| `netrunner_acquisition_dedup_total` | counter | `method` | Deduplication events |
| `netrunner_acquisition_cover_art_fetch_total` | counter | `source`, `outcome` | Cover art fetch attempts |

### Prometheus Scrape Config

The server exposes `/metrics` on `:8080` and the worker on `:9090`:

```yaml
scrape_configs:
  - job_name: "netrunner-server"
    static_configs:
      - targets: ["localhost:8080"]
    metrics_path: /metrics
    scrape_interval: 15s
  - job_name: "netrunner-worker"
    static_configs:
      - targets: ["localhost:9090"]
    metrics_path: /metrics
    scrape_interval: 15s
```

### Grafana Dashboard

Import `ops/grafana/netrunner-dashboard.json` into Grafana for a pre-built dashboard covering worker throughput, job latency percentiles, external API health, and acquisition pipeline metrics.

## 🤝 Contributing

We welcome contributions that align with our "Console-First" and "Standalone" design principles. 

**For AI Agents**: Please read [AGENTS.md](./AGENTS.md) for architectural invariants and development constraints before proposing changes.

## 📝 License

MIT License - see [LICENSE](LICENSE) for details.

---
**Architecture**: Go 1.25+, PostgreSQL (SQLite for NetrunnerLite), Fiber, HTMX

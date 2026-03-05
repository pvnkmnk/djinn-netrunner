# Technology Stack: Djinn NETRUNNER

## Languages
- **Python 3.12+:** Primary language for the web console and background worker.
- **Go 1.25+:** Used for high-performance core components and task orchestration.
- **JavaScript (Vanilla):** Minimal client-side logic for HTMX and WebSockets.

## Frameworks & Libraries
- **FastAPI:** Python web framework for the management console.
- **Fiber:** Go web framework for core services.
- **HTMX:** For seamless, server-rendered UI updates without full page reloads.
- **asyncio:** Core Python library for concurrent operations in the worker.
- **Asynq:** Go library for distributed task processing via Redis.
- **Spotipy:** Python library for Spotify Web API integration.

## Data Layer
- **PostgreSQL 16:** Primary database for state storage, job persistence, and locking.
- **LISTEN/NOTIFY:** PostgreSQL feature for real-time event-driven updates.
- **Redis:** Used by Asynq for task queue management.

## Infrastructure
- **Docker & Docker Compose:** Containerization and orchestration for all services.
- **Caddy:** Reverse proxy with automatic TLS management.
- **Soulseek (slskd):** Daemon for music acquisition integration.
- **Gonic:** Subsonic-compatible streaming server.
- **MusicBrainz API:** For enhanced metadata enrichment.

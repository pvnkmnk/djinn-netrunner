# Technology Stack: Djinn NETRUNNER

## Languages
- **Go 1.25+:** Primary language for the web API, background worker, and task orchestration.
- **JavaScript (Vanilla):** Minimal client-side logic for HTMX and WebSockets.

## Frameworks & Libraries
- **Fiber:** Go web framework for all backend services.
- **HTMX:** For seamless, server-rendered UI updates.
- **GORM:** For database ORM and migrations.
- **id3v2 & go-flac:** For high-fidelity metadata and cover art embedding.
- **Music Discovery APIs:** Spotify, Last.fm, ListenBrainz, and Discogs integrations for automated acquisition.
- **gofeed:** For high-performance RSS/Atom feed parsing.

## Data Layer
- **SQLite 3:** Primary database for standalone and distributed deployments.
- **LiteFS:** Distributed SQLite engine for multi-node coordination.
- **PostgreSQL 16:** Optional database for large-scale multi-user deployments.

## Infrastructure
- **Docker & Docker Compose:** Containerization and orchestration for all services.
- **Caddy:** Reverse proxy with automatic TLS management.
- **LiteFS (FUSE):** For transparent database replication across clusters.
- **Soulseek (slskd):** Daemon for music acquisition integration.
- **Gonic:** Subsonic-compatible streaming server.
- **MusicBrainz API:** For enhanced metadata enrichment.

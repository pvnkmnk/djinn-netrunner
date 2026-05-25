# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.1] — 2026-05-XX

Initial release of Djinn NetRunner.

### Added

- **Core Platform**
  - Go 1.25 backend with Fiber HTTP framework and HTMX/Pongo2 server-rendered UI
  - Background worker orchestrator with goroutine-based job processing
  - MCP (Model Context Protocol) agent interface with 20 registered tools
  - CLI management tool (`netrunner-cli`) for operational scripting

- **Music Acquisition Pipeline**
  - Automated discovery from Spotify playlists, Last.fm loved tracks, ListenBrainz, RSS feeds, and local files
  - Soulseek acquisition via slskd integration with search, download, and import
  - Quality profiles with configurable format, bitrate, and lossless preferences
  - Peer reputation scoring for Soulseek download source selection

- **Metadata & Enrichment**
  - MusicBrainz, AcoustID, Discogs, Last.fm, and ListenBrainz provider integrations
  - Audio fingerprinting for track identification
  - Metadata tagging and cover art fetching
  - Persistent metadata cache with configurable expiry

- **Library Management**
  - Multi-library support with filesystem scanning and indexing
  - Track pruning for orphaned database records
  - Gonic and Navidrome Subsonic server integration
  - Disk quota tracking per library

- **Monitoring & Notifications**
  - Artist monitoring with MusicBrainz release tracking
  - Webhook notifications for job completion and errors
  - Real-time WebSocket event streaming and job console

- **Security**
  - Session-cookie authentication with role-based access control (user/admin)
  - BOLA (Broken Object Level Authorization) enforcement on all resource endpoints
  - WebSocket ownership enforcement for job log streaming
  - HTTPOnly, Secure, SameSite cookie flags under Caddy TLS
  - Centralized proxy-aware HTTP client factory (`NewProxyAwareHTTPClient`)
  - Auth endpoint rate limiting

- **Infrastructure**
  - Docker Compose stack: PostgreSQL 16, slskd, Gonic, Caddy reverse proxy
  - SQLite WAL support for zero-config local development
  - LiteFS guard for primary node detection in clustered deployments
  - PostgreSQL advisory locks for safe concurrent worker job claims
  - GORM auto-migrations with PostgreSQL enum-to-text conversion support

- **Reliability**
  - Worker panic recovery with structured error logging
  - Graceful shutdown with configurable drain timeout
  - Zombie job cleanup via heartbeat-based detection
  - Context nil guard in worker constructor

- **Documentation**
  - AGENTS.md with full repository map, API reference, MCP tool schemas, and database driver behavior
  - Ops runbooks: backup, upgrade, disaster recovery, SQLite-to-Postgres migration
  - Database support tier guidance (SQLite WAL / PostgreSQL 16 / LiteFS)
  - Startup warning when SQLite used with concurrent worker configuration

- **CI/CD**
  - GitHub Actions: unit tests, go vet, vulnerability scanning, coverage upload
  - Integration test workflow with dockerized Postgres and slskd
  - Docker build and push to GitHub Container Registry (ghcr.io)
  - PR review automation (PRGuard, PR-Sentry, Sourcery)

[Unreleased]: https://github.com/pvnkmnk/djinn-netrunner/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/pvnkmnk/djinn-netrunner/releases/tag/v0.0.1

# Djinn NETRUNNER — Architecture & Contracts

This document defines the runtime contracts and invariants for NETRUNNER, especially the worker/DB correctness model.

## Services
- **caddy**: Edge proxy and TLS termination.
- **SQLite (WAL)**: Primary system-of-record for jobs, logs, metadata, and concurrency primitives (PostgreSQL also supported).
- **ops-web (Go/Fiber)**: Management API + server-rendered templates + HTMX UI; WebSockets for console streaming.
- **ops-worker (Go)**: Background job orchestrator with native goroutine concurrency, heartbeats, and reaper.
- **slskd**: Acquisition daemon with bounded download slots.
- **gonic**: Streaming server (Subsonic-compatible).

## Core Data Model
### Tables (minimum)
- **jobs**: Durable job records (state machine + execution metadata).
- **jobitems**: Durable units of work; MUST be created before execution (deterministic plan).
- **joblogs**: Append-only console lines for each job.
- **acquisitions**: Provenance and final-path record for imported items.
- **metadata_cache**: Persistent shadow cache for external API responses (MusicBrainz/Spotify).

## Concurrency + Correctness Invariants
1. **Contention-Safe Claims**: Jobs and jobitems are claimed using atomic status updates (SQLite) or `FOR UPDATE SKIP LOCKED` (PostgreSQL) to prevent duplicate claims.
2. **Explicit Exclusivity**: Per-scope locks (file-based or DB-level) prevent multiple workers from executing the same scope (e.g., syncing the same playlist) simultaneously.
3. **Deterministic Work Plans**: `jobitems` are created before execution; retries resume without re-deriving metadata.
4. **Fair Scheduling**: Round-robin task selection across active jobs to prevent starvation.
5. **Authoritative Heartbeats**: Running jobs update `heartbeat_at` frequently; the reaper uses this to detect and recover from worker crashes.

## Agentic Interface (MCP)
NetRunner implements an embedded **Model Context Protocol (MCP)** server. This allows AI agents to:
- **Probe System**: Check connectivity and resource health.
- **Manage Watchlists**: Add or list automated discovery sources.
- **Monitor Pipeline**: View real-time job logs and statuses.
- **Search Library**: Query the combined Gonic and local indices.

## DB Connection Model
The system uses a unified GORM connection with specific optimizations for SQLite:
- **WAL Mode**: Enabled for high-concurrency read/write operations.
- **Busy Timeout**: Configured to 5000ms to prevent locking issues.
- **Synchronous**: Set to `NORMAL` for performance while maintaining crash-safety.


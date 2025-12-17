# Djinn NETRUNNER - Quick Start Guide

## Prerequisites

- Docker & Docker Compose
- Git

## Setup

1. **Clone and configure**
   ```bash
   cd netrunner_repo
   cp .env.example .env
   ```

2. **Edit `.env`** with your credentials:
   - Set secure `POSTGRES_PASSWORD`
   - Add your Soulseek `SLSKD_USERNAME` and `SLSKD_PASSWORD`
   - Generate a random `SLSKD_API_KEY`
   - Set `DOMAIN` (use `localhost` for local development)

3. **Start the stack**
   ```bash
   docker compose up -d
   ```

4. **Check services are healthy**
   ```bash
   docker compose ps
   docker compose logs -f
   ```

## Access

- **Operations Console**: https://localhost (or your configured domain)
- **Gonic Streaming**: https://localhost/music
- **slskd Interface**: http://localhost:5030

## First Steps

### Option 1: Using the example playlist

1. **Copy example playlist to container**
   ```bash
   docker compose exec ops-worker mkdir -p /data/playlists
   docker compose cp examples/playlist_example.txt ops-worker:/data/playlists/favorites.txt
   ```

2. **Add source using CLI tool**
   ```bash
   docker compose exec ops-worker python /app/add_source.py \
     "postgresql://musicops:yourpassword@postgres:5432/musicops" \
     file_list \
     /data/playlists/favorites.txt \
     "Example Favorites"
   ```

3. **Access the ops console** at https://localhost

4. **Click SYNC** on the source to start acquisition

5. **Watch the console** stream logs in real-time as tracks are searched, downloaded, and imported

### Option 2: Using the Web UI

1. **Access the ops console** at https://localhost

2. **Click "+ ADD"** in the SOURCES section

3. **Fill in the source form**:
   - Display Name: "My Favorites"
   - Source Type: file_list
   - Source URI: /data/playlists/favorites.txt
   - Enable sync: ✓

4. **Click SYNC** to start acquisition

5. **Watch the console** stream live logs

See `SOURCE_MANAGEMENT_UI.md` for detailed UI documentation.

## Architecture Overview

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

## Key Features

- **Console-first UX**: Logs are the primary progress visualization
- **HTMX UI**: Server-rendered with minimal JavaScript
- **PostgreSQL-backed**: All state in DB with LISTEN/NOTIFY
- **Round-robin fairness**: Multiple jobs progress simultaneously
- **WebSocket streaming**: Live console output with attach modes
- **Crash-safe**: Advisory locks + heartbeats + reaper

## Troubleshooting

Check service logs:
```bash
docker compose logs ops-web
docker compose logs ops-worker
docker compose logs postgres
```

Database access:
```bash
docker compose exec postgres psql -U musicops -d musicops
```

Check running jobs:
```sql
SELECT id, jobtype, state, started_at, heartbeat_at
FROM jobs
WHERE state = 'running';
```

## Documentation

- `docs/ARCHITECTURE.md` - System architecture and invariants
- `docs/UIIMPLEMENTATION.md` - Console patterns and HTMX contracts
- `docs/RUNBOOK.md` - Operational procedures
- `AGENTS.md` - Development guidelines

## Job Types

NETRUNNER supports the following job types:

1. **sync** - Syncs a playlist/source and creates acquisition jobs
2. **acquisition** - Searches slskd and downloads tracks
3. **import** - Validates and imports downloaded files to organized library
4. **index_refresh** - Triggers Gonic library scan to update streaming index

Jobs are executed with:
- Round-robin fairness (multiple jobs progress simultaneously)
- Crash-safe recovery via heartbeats and reaper
- Advisory locks for per-scope exclusivity

## Workflow

```
Source Sync → Acquisition Job Created → Items Queued
                                              ↓
                                        Search slskd
                                              ↓
                                        Download Files
                                              ↓
                                        Extract Metadata
                                              ↓
                                        Import to Library
                                              ↓
                                        Index Refresh (Gonic)
                                              ↓
                                        Ready to Stream!
```

## Advanced Usage

### Manual job creation

```sql
-- Create a manual index refresh job
INSERT INTO jobs(jobtype, scope_type, scope_id)
VALUES ('index_refresh', 'library', 'main');

-- Create acquisition job with custom items
INSERT INTO jobs(jobtype, scope_type, scope_id)
VALUES ('acquisition', 'manual', 'batch-001')
RETURNING id;

-- Add items to the job
INSERT INTO jobitems(job_id, sequence, normalized_query, artist, track_title)
VALUES
  (123, 0, 'Beatles Yesterday', 'The Beatles', 'Yesterday'),
  (123, 1, 'Pink Floyd Time', 'Pink Floyd', 'Time');
```

### Monitoring

```sql
-- Active jobs with progress
SELECT
  j.id,
  j.jobtype,
  j.state,
  j.started_at,
  COUNT(*) FILTER (WHERE i.status = 'imported') as completed,
  COUNT(*) as total
FROM jobs j
LEFT JOIN jobitems i ON j.id = i.job_id
WHERE j.state = 'running'
GROUP BY j.id;

-- Recent failures
SELECT id, jobtype, summary, finished_at
FROM jobs
WHERE state = 'failed'
ORDER BY finished_at DESC
LIMIT 10;
```

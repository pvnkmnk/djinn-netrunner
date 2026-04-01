# backend/cmd/worker/

## Responsibility
Background job processing worker that executes acquisition, sync, scan, and other long-running tasks. Implements the round-robin job dispatch model with advisory locking, heartbeat monitoring, and zombie job recovery. Runs as a long-lived process alongside the server.

## Design
- **Entry Point**: `main.go` - WorkerOrchestrator struct with service composition
- **Orchestration Pattern**: `WorkerOrchestrator` holds all services and manages job lifecycle
- **Concurrency Model**: Max 5 concurrent jobs (`MaxConcurrentJobs`), processed round-robin
- **Job Types**: 
  - `acquisition` - Downloads tracks via slskd (itemized, one per job item)
  - `sync` - Watchlist synchronization
  - `scan` - Library filesystem scanning
  - `enrich` - Metadata enrichment via Discogs
  - `artist_scan` - Discography sync for monitored artist
  - `release_monitor` - Check for new releases
  - `index_refresh` - Gonic index refresh
- **Locking**: Advisory locks via `LockManager` for scope-level exclusivity
- **Heartbeat**: Updates job heartbeats every 5 seconds
- **Recovery**: Zombie cleanup loop resets jobs with stale heartbeats (>2 min)
- **Notification**: Webhook notifications for job completion and quota warnings

## Flow
1. Load config and connect to database
2. Create `WorkerOrchestrator` with all services initialized:
   - MusicBrainzService, ArtistTrackingService, ReleaseMonitorService
   - WatchlistService, ScannerService, DiscogsService
   - SpotifyService, SlskdService, MetadataExtractor
   - LockManager, LiteFSGuard, NotificationService, DiskQuotaService
3. Call `worker.Start()`:
   - If LiteFS primary: start scheduler loop, watchlist polling loop, zombie cleanup, release monitor
   - If Postgres: start LISTEN loop for wakeup notifications
   - Main loop: claim and process jobs round-robin every 5 seconds
4. `claimAndProcess()`: transactionally claims queued job, acquires advisory lock
5. `processActiveJobsRoundRobin()`: dispatches goroutines based on job type
   - `acquisition`: calls `AcquisitionHandler.ExecuteItem()` per job item
   - `sync`: calls `SyncHandler.Execute()`
   - other types: calls `runMonolithicJob()`
6. Job completion: releases lock, updates job state, triggers notification
7. On shutdown: cancel active jobs, wait for goroutines, close lock manager

## Integration
- **Depends On**: `internal/config`, `internal/database`, `internal/services`, `internal/api`
- **External**: slskd (Soulseek download), Gonic (music server), MusicBrainz (metadata), Discogs (metadata), Spotify (playlists)
- **What Depends On It**: Server (creates jobs), Database (job state machine), Operators (monitors via logs)

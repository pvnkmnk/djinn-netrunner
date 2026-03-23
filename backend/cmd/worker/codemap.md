# backend/cmd/worker/

## Responsibility
Background job processor. Round-robin dispatch with concurrent execution, heartbeat monitoring, zombie recovery, and cron-based scheduling.

## Design
- `WorkerOrchestrator` struct holds all services, handlers, and active job tracking
- `MaxConcurrentJobs = 5` — semaphore via buffered channel
- Round-robin claim: `claimNextJob()` uses `FOR UPDATE SKIP LOCKED` for fair distribution
- Job handlers: `SyncHandler` (watchlist sync) and `AcquisitionHandler` (download pipeline)
- Heartbeat goroutine updates `last_heartbeat` every 15s
- Zombie reaper every 60s — stale heartbeats → reset to `pending`
- Cron scheduler (`robfig/cron`) for scheduled watchlist syncs
- Advisory locking via `database.LockManager` for per-scope exclusivity

## Flow
1. Config → database → initialize all services → create `WorkerOrchestrator`
2. Start heartbeat, zombie reaper, cron scheduler goroutines
3. Main loop: `claimNextJob()` → acquire advisory lock → dispatch to handler → release lock
4. SyncHandler: fetch sources → diff → create job items → execute
5. AcquisitionHandler: search slskd → download → extract metadata → fingerprint → embed art → move to library
6. Graceful shutdown: cancel context → wait active jobs → exit

## Integration
- **Consumes**: All `internal/services`, `internal/database` (LockManager, LiteFSGuard), `internal/config`
- **External**: slskd (P2P), Gonic, MusicBrainz, AcoustID, Discogs, Spotify

***
skill: debugging
version: 1
repo: netrunner
language: go
tags: [debugging, logs, worker, diagnostics]
***

# Skill: Debug NetRunner

## Purpose
Diagnose runtime issues in NetRunner across API, worker orchestration, and integrations.

## Prerequisites
- Repro steps and failing command/route.
- Access to app logs and DB state.

## Core Concepts
- Worker state machine depends on `jobs`, `jobitems`, heartbeats, and advisory locks.
- API auth failures are usually missing/expired `session_id` or ownership filtering.
- Integrations (slskd, Spotify, MusicBrainz, Discogs) can fail independently.

## Step-by-Step Procedures
1. Capture failing context:
```bash
docker compose logs -f netrunner
```
2. Verify health and connectivity:
```bash
curl http://localhost:8080/api/health
cd backend
go run ./cmd/cli status
```
3. Inspect queue/job state in DB.
```sql
SELECT id, job_type, state, heartbeat_at, error_detail
FROM jobs
ORDER BY requested_at DESC
LIMIT 25;
```
4. Reproduce with targeted test.
```bash
cd backend
go test ./internal/<package> -run <TestName> -v
```
5. Apply patch and rerun scoped tests, then broader validation.

## Code Patterns
Structured logging with context:
```go
slog.Error("Error claiming job", "worker_id", w.workerID, "job_id", job.ID, "error", err)
```

## Validation
- Error is reproducible before fix and absent after fix.
- Relevant job/API tests pass.
- No new auth/ownership regressions.

## Edge Cases & Error Handling
- Zombie jobs: check `state='running'` with stale `heartbeat_at`.
- Path validation: library path handlers require absolute paths.
- For external API failures, verify rate limiting and credentials before code changes.

## References
- `backend/cmd/worker/main.go`
- `backend/internal/services/job_handlers.go`
- `backend/internal/api/`
- `backend/internal/services/safe_http.go`
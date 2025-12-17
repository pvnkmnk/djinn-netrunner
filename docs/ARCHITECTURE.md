# Djinn NETRUNNER — Architecture & Contracts

This document defines the runtime contracts and invariants for NETRUNNER, especially the worker/DB correctness model.

## Services
- caddy: edge proxy and TLS termination
- postgres: system-of-record for jobs, logs, metadata, and concurrency primitives
- ops-web: FastAPI + server-rendered templates + HTMX UI; WebSockets for console streaming
- ops-worker: asyncio job runner with fairness scheduling, heartbeats, and reaper
- slskd: acquisition daemon with bounded download slots
- gonic: streaming server (Subsonic-compatible)

## Core data model
### Tables (minimum)
- jobs
  - Purpose: durable job records (state machine + execution metadata)
  - Common fields: id, jobtype, state, requested_at, started_at, finished_at, heartbeat_at, worker_id, attempt, summary
- jobitems
  - Purpose: durable units of work; MUST be created before execution (deterministic plan)
  - Common fields: id, jobid, status, normalizedquery, slskd ids, failure_reason
- joblogs
  - Purpose: append-only console lines for each job
- acquisitions
  - Purpose: provenance and final-path record for imported items

### Job state machine (canonical)
queued → running → (succeeded | failed | cancelled)

### Job item state machine (canonical)
queued → searching → downloading → (imported | skipped | failed)

## Concurrency + correctness invariants
1. Claims are contention-safe
   - Jobs and jobitems are claimed using FOR UPDATE SKIP LOCKED patterns to prevent duplicate claims across workers.
2. Exclusivity is explicit
   - Per-scope advisory locks prevent two workers from executing the same scope simultaneously.
3. Work plans are deterministic
   - jobitems are created before execution; retries must resume without re-deriving.
4. Fairness is intentional
   - One item per active job per scheduling turn (round-robin) to prevent starvation.
5. Heartbeats are authoritative
   - Running jobs update heartbeat frequently; reaper uses heartbeat age to detect staleness.

## DB connection model (worker)
The worker uses multiple DB connections:
- notifyconn
  - Autocommit
  - LISTEN on a wakeup channel (e.g., opswakeup)
- lockconn
  - Autocommit
  - Holds advisory locks while a job scope is running
- db / short transactions
  - Short-lived or pooled connections for claims and updates using BEGIN/COMMIT
- maintenance (reaper) connection — IMPORTANT
  - Short-lived connection that is opened for reaper ticks and then closed
  - Closing the session guarantees lock-check locks drop even if an error occurs

## Advisory lock namespaces (example)
- Namespace 1001: per-playlist/source sync scope lock
- Additional namespaces may be added for other scope types; each must be documented and used consistently by worker + reaper.

## Heartbeat + reaper contract
- Workers heartbeat running jobs on a short cadence (e.g., 1–5 seconds).
- Reaper runs on a slower cadence (e.g., 30–60 seconds) and:
  1. Finds running jobs with stale heartbeat (e.g., older than 10 minutes)
  2. Validates scope ownership via lock-check
  3. Requeues the job if retries remain, else fails it
  4. Optionally resets in-flight jobitems back to queued to allow clean resume

### Policy decision (mandatory)
Run reaper logic ONLY on the short-lived maintenance connection (connect → reap → close).
Do not run reaper calls on lockconn.

## Event-driven UI contract
- Workers append log lines to joblogs.
- Workers emit NOTIFY events (e.g., opsevents) to indicate “new log available.”
- ops-web listens and fans out updates to connected WebSocket clients.
- HTMX updates are used for dashboard panels (stats/playlists), while console output streams via WebSockets.

## Adding a new job type (checklist)
- Extend jobs.jobtype constraint/enum.
- Implement worker handler:
  - claim job (SKIP LOCKED)
  - acquire scope lock
  - progress jobitems safely
  - write joblogs
  - finish job + release lock
- Add UI trigger endpoint in ops-web.
- Emit worker wakeup NOTIFY after enqueueing jobs.
- Update docs/ARCHITECTURE.md and docs/RUNBOOK.md.

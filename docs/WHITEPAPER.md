# Djinn NETRUNNER — Production White Paper

Djinn NETRUNNER is a self-hosted music acquisition and streaming appliance that unifies playlist/source synchronization, download orchestration, enrichment/import, and playback into a single operator-first system.

The core UX is a “protocol console”: structured logs are the primary progress surface instead of progress bars.

## Goals
- Provide a reliable, observable pipeline from playlist/source → acquired files → organized library → streamable catalog.
- Keep critical state in PostgreSQL so crashes/restarts are recoverable and explainable.
- Maintain predictable concurrency and fairness under multiple simultaneous jobs.

## Non-goals
- Not a general-purpose media manager with heavy client apps or a large SPA frontend.
- Not a distributed queue platform; external queues/caches are intentionally avoided in the baseline design.

## System overview
NETRUNNER runs as a small container stack:
- Caddy: edge proxy + TLS termination
- PostgreSQL: system-of-record (jobs, logs, metadata, concurrency primitives)
- ops-web: operations UI + API (Go/Fiber + HTMX + server-rendered templates) and WebSockets for console streaming
- ops-worker: async orchestration (job claims, locks, round-robin dispatch, heartbeats, reaper)
- slskd: acquisition daemon (Soulseek)
- Gonic: Subsonic-compatible streaming

The system uses:
- PostgreSQL row-level locking (SKIP LOCKED) for safe claiming
- PostgreSQL advisory locks for per-scope exclusivity
- PostgreSQL LISTEN/NOTIFY for event-driven wakeups and UI fanout

## Pipeline flow (end-to-end)
1. Source registration
   - ops-web stores tracked sources and their sync metadata.
2. Planning (deterministic)
   - ops-web materializes a durable job plan: a jobs row + jobitems rows before execution.
3. Execution (worker)
   - ops-worker claims jobs safely, acquires a per-scope advisory lock, and progresses work in round-robin slices across active jobs.
4. Acquisition
   - slskd executes downloads under a global concurrency cap.
5. Import/enrichment
   - Completed items are validated and moved into the library layout.
   - Index refresh is event-driven (no periodic scanning requirement in the default posture).
6. Observability
   - Job logs are appended to joblogs and streamed live to the UI console via WebSockets fed by NOTIFY fanout, filtered per-job_id subscription.

## Concurrency model
NETRUNNER combines:
- FOR UPDATE SKIP LOCKED for contention-safe claiming of jobs and job items.
- Session advisory locks for exclusivity per playlist/source/library scope (crash-safe on connection drop).
- A round-robin dispatcher that advances one item per active job per scheduling turn to prevent starvation.

## Reliability model
- Running jobs heartbeat periodically.
- A reaper detects stale heartbeats and requeues jobs when safe.
- Retries must preserve determinism: requeued jobs resume from existing jobitems rather than regenerating work.

## “Console-first” UX
The console is a first-class operations surface:
- It streams logs live.
- It supports “attach modes” so operators can see liveness without being spammed by backlog replays.
- It avoids heavy UI primitives (spinners, progress bars) in favor of terminal semantics.

## Deployment posture (production)
- Caddy terminates TLS and routes traffic to ops-web and Gonic.
- PostgreSQL is not exposed publicly.
- Secrets are provided via environment variables and should be rotated and locked down as part of standard ops hygiene.

## Extensibility
A new job type typically requires:
- Schema constraint/enum extension for the job type
- Worker handler implementation in the same locking + fairness model
- UI trigger endpoint + worker wakeup NOTIFY
- Documentation updates (ARCHITECTURE + RUNBOOK as needed)

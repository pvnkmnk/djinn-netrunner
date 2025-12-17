# AGENTS.md — Djinn NETRUNNER

AGENTS.md is the canonical “README for agents” used by agentic IDE workflows (Cursor, etc.).
If instructions conflict, follow .cursor/rules/*.mdc first, then the docs in docs/.

## Project identity
NETRUNNER is a console-first operations UI for a music acquisition pipeline.
Logs are the primary progress visualization (do not replace with progress bars/spinners).

## Non-negotiable constraints
- HTMX + server-rendered partials (no SPA frameworks).
- Single CSS file and minimal JS for console controls.
- PostgreSQL is the system-of-record; avoid adding Redis/external queues.
- Deterministic work plans: jobitems are created before execution; retries resume and never re-derive.
- Event-driven updates: LISTEN/NOTIFY + WebSockets, not DB polling for console output.

## Key invariants (backend)
- Claims use FOR UPDATE SKIP LOCKED.
- Per-scope exclusivity uses Postgres advisory locks (session-level).
- Worker heartbeats running jobs frequently; a reaper requeues stale jobs.
- Reaper calls must run on a short-lived maintenance DB connection (connect → reap → close).

## Where to look
- docs/ARCHITECTURE.md — locking model, state machines, invariants
- docs/UIIMPLEMENTATION.md — console socket + attach modes + minimal JS contract
- docs/RUNBOOK.md — operational procedures and failure modes

## How agents should work (practical)
1. Locate the contract in docs/ARCHITECTURE.md or docs/UIIMPLEMENTATION.md.
2. Make the smallest change that preserves invariants.
3. Update docs if behavior changes.
4. Provide a short test checklist (even if tests are manual).

## “Do not do” list
- Do not introduce React/Vue/SPA routing.
- Do not split CSS into multiple files.
- Do not move transient state into the DB as a substitute for locks.
- Do not change lock namespaces/keys without updating worker + reaper + docs.

# AGENTS.md — Djinn NETRUNNER

AGENTS.md is the canonical “README for agents” used by agentic IDE workflows (Cursor, Claude Code, etc.).
If instructions conflict, follow .cursor/rules/*.mdc first, then the docs in docs/.

## Project identity
NETRUNNER is a console-first operations UI for a music acquisition pipeline.
Logs are the primary progress visualization (do not replace with progress bars/spinners).

## Non-negotiable constraints
- **Language**: Go 1.25+
- **Architecture**: Single-binary focus with embedded SQLite (CGO-free via `modernc.org/sqlite`).
- **Frontend**: HTMX + server-rendered Fiber templates + Vanilla CSS (No SPA frameworks).
- **Concurrency**: Native goroutines for job workers; round-robin orchestration.
- **Database**: SQLite in WAL mode is the system-of-record.
- **Privacy**: All P2P/API traffic must support SOCKS5/HTTP proxying.

## Key invariants (backend)
- **Persistence**: Use GORM for all database interactions to maintain SQLite/Postgres compatibility.
- **Exclusivity**: Per-scope exclusivity uses file-based or database-level locks (see `internal/database/locks.go`).
- **Jobs**: Job items are created before execution; retries resume and never re-derive state.
- **Real-time**: Events are broadcast via WebSockets from the Fiber API.

## Where to look
- `docs/ARCHITECTURE.md` — locking model, state machines, invariants.
- `docs/UIIMPLEMENTATION.md` — console socket + attach modes + minimal JS contract.
- `docs/RUNBOOK.md` — operational procedures and failure modes.
- `backend/internal/services/` — Core logic for slskd, Gonic, and Watchlists.
- `backend/cmd/agent/` — MCP server implementation.

## How agents should work (practical)
1. **Scout**: Check `internal/database/models.go` before changing schema.
2. **Preserve**: Make the smallest change that preserves architectural invariants.
3. **Document**: Update `docs/` if logic or behavior changes.
4. **Agentic Surface**: Ensure any new core feature is exposed via both the CLI and the MCP server.

## “Do not do” list
- Do not introduce React/Vue/SPA routing.
- Do not split CSS into multiple files.
- Do not add external heavy queues (Redis/RabbitMQ).
- Do not bypass the `WatchlistService` for source management.

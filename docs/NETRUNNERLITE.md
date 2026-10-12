# NetrunnerLite capability boundary

Audited 2026-10-09 for ADR 0004 rollout item 4. ADR 0004 §2 states the two editions in one line
per row; this document is the per-feature audit that turns those rows into a promise instead of a
direction. Every claim below was read out of the code at the cited location, and where the
behaviour was in question it was measured by running the binary — the commands and their observed
output are in "How this was verified".

## How the edition is selected

- `database.IsPostgres(DATABASE_URL)` is a prefix check for `postgres://` / `postgresql://`
  (`backend/internal/database/connection.go:67`). Every other value takes the SQLite path:
  **NetrunnerLite is selected by `DATABASE_URL` — not by a flag, build tag, image, or separate
  binary.**
- One codebase, one set of binaries. The entire engine divergence in Go is five places: the
  `IsPostgres` branches at `connection.go:20`, `connection.go:51` (SQLite PRAGMAs) and
  `websocket.go:75` (log fan-out), the dialector check at `backend/cmd/worker/main.go:250`
  (wakeup listener), and the `TableLockManager` wiring. Anything not on that list behaves
  identically on both editions.
- `/api/health` is the same route and the same contract on both: every reported check carries a
  `status`. With no slskd key and no library path configured, SQLite answers
  `{"status":"ok","checks":{"database":{"status":"ok"}}}`. Assert the contract, never a fixed key
  set.

## The matrix

"Same" means the behaviour is identical **by construction** — no engine-specific branch exists —
not merely that it happened to pass on SQLite.

| Feature | Djinn-Netrunner (PostgreSQL) | NetrunnerLite (SQLite) | Verdict |
|---|---|---|---|
| Server, UI, session auth | Fiber + HTMX, `session_id` cookie, roles `user`/`admin` | identical | **Same** |
| Watchlist ingest (Spotify, Last.fm, ListenBrainz, RSS, local) | provider interface, DB-agnostic | identical | **Same** |
| Acquisition pipeline (slskd search → download → library) | same stages and gates | identical code path | **Same** |
| Per-scope exclusivity while processing | real session-level `pg_try_advisory_lock` (`locks.go:107`) | `TableLockManager`: a row with a 15-minute TTL | **Lite is weaker** |
| Concurrent jobs per worker | `MAX_CONCURRENT_JOBS` (default 5) | **one**; `MAX_CONCURRENT_JOBS > 1` warns, it does not clamp | **Lite limit** |
| Job wakeup latency | 5-second poll, plus a `LISTEN/NOTIFY` fast path | 5-second poll | **Same today** (see "dormant fast path") |
| Multi-node / horizontal scale | supported | not promised; LiteFS retired (ADR 0001) | **Lite limit** |
| Multi-user data scoping | plain `owner_user_id` filters plus role checks | identical | **Same** |
| Subsonic / OpenSubsonic `/rest/*` | full surface | identical; the only engine-sensitive SQL is `ORDER BY RANDOM()` (`subsonic.go:1378`, `:1439`), which is portable. The whole group is gated by config (`cfg.Subsonic.Enabled`, `cmd/server/main.go:491`), not by edition | **Same** |
| Aggregate stats (`/api/stats/*`, CLI `stats`) | `SUM(CASE WHEN …)` aggregates, deliberately not `FILTER (WHERE …)` | identical, and measured working | **Same** |
| Schema evolution | GORM `AutoMigrate` + `database.Migrate` backfills, portable SQL | identical | **Same** |
| `netrunner-cli` and MCP tools (`cmd/agent`) | the same command and tool set: read-only probes plus stateful writes | identical | **Same** |
| Live job-log / event streaming | backlog on connect; continuation needs a NOTIFY publisher that is not installed | backlog on connect, then silence | **Neither, today** |

## What NetrunnerLite does not promise

1. **One worker process, one job at a time.** SQLite is single-writer. The worker *warns*
   (`SQLite detected with MaxConcurrentJobs > 1 — concurrent workers are unsafe without
   PostgreSQL advisory locks`) and keeps running — measured with `MAX_CONCURRENT_JOBS=2`. The
   warning is the edition's limit; it is not enforced.
2. **No cross-process lock safety.** Postgres takes a real session-level advisory lock. Lite
   writes a `locks` row (`Lock` at `models.go:484`, where `Key` is the primary key) with a
   15-minute `expires_at`; `AcquireTryLock` asks "is there an unexpired row?" and then `Save`s.
   Two callers that pass that check in the same window both report success, and the second
   overwrites the first's row — so this is best-effort exclusion inside one process, not a
   guarantee. It is adequate for the single-worker deployment Lite promises and must not be relied
   on beyond that.
3. **No live push.** See the WebSocket row and the console note below.

## Gaps that are not edition differences

Recorded here so the Lite story is not blamed for them, and so nobody "fixes Lite" by chasing them.

- **The NOTIFY fast path has no publisher in a composed deployment.** The worker listens on
  `opswakeup` (`worker/main.go:331`) and the web server listens on `opsevents` for log fan-out
  (`websocket.go:89`), but the only sender is a SQL trigger in
  `ops/db/init/02-functions.sql` (`PERFORM pg_notify('opsevents'|'opswakeup', …)`). No compose file
  mounts `ops/db/init/`, no Go code executes it, and no Go code issues `NOTIFY`. Wakeups and log
  fan-out therefore fall back to the worker's 5-second job poll (`worker/main.go:272`) and to the
  UI's htmx polling on **both** editions. Install that SQL (or publish from the application) and
  Postgres gains the fast path; Lite does not, and never will.
- **The console socket is miswired.** `ops/web/templates/index.html:109` points htmx's `ws`
  extension at `/ws/jobs`, while the registered route is `/ws/jobs/:job_id`
  (`cmd/server/main.go:465`), and `/console/attach` answers "Not attached: no job is selected"
  (DJI-562). Log viewing works on both editions through `GET /partials/job-logs?job_id=`; the
  *live* console is wired on neither.
- **The SQL bootstrap is documentation, not a live step.** `ops/db/init/01-schema.sql` defines
  five tables and no `users`. The live schema comes from `database.Migrate` + `AutoMigrate`. The
  Go tests that duplicate migration DDL exist precisely because reading the SQL file proves it
  parses, not that its statements do what the comment claims.

## How this was verified

Measured 2026-10-09 on this host, binaries built from the working tree, against a fresh SQLite file
(`DATABASE_URL=<path>/lite-audit.db`):

```
server : go build ./cmd/server  -> booted on SQLite; GET /api/health -> 200
         {"status":"ok","checks":{"database":{"status":"ok"}}}
         log: "Skipping Postgres log listener for non-Postgres database"
worker : go build ./cmd/worker  -> still running after 10s with MAX_CONCURRENT_JOBS=2
         log: "Using table-based lock manager for SQLite"
         log: WARN "SQLite detected with MaxConcurrentJobs > 1 ..." max_concurrent_jobs=2
         log: scheduler, zombie cleanup, watchlist polling and staging janitor loops started
cli    : go build ./cmd/cli     -> `stats jobs` and `stats summary` both succeed on SQLite
static : rg 'FILTER (WHERE'     -> one comment (migrate.go:171), no live SQL
         rg 'pg_notify'         -> ops/db/init/02-functions.sql + tests only
         rg 'db/init'           -> docs, codemaps, skills, two test comments; no compose mount
```

## Keeping it true

- A feature that gains an `IsPostgres` branch belongs in the matrix with a verdict.
- If the `opswakeup` / `opsevents` fast path is made real, correct the wakeup row here, the "Job
  wakeup" note in `README.md`, and ADR 0004 §2.
- If Lite's lock manager gains an ownership-checked release or a monotonic `expires_at`, state the
  new guarantee instead of quietly deleting the caveat.

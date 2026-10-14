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
- One codebase, one set of binaries. The entire engine divergence in Go is six places: the
  `IsPostgres` branches at `connection.go:20`, `connection.go:51` (SQLite PRAGMAs) and
  `websocket.go:75` (log fan-out), the dialector check at `backend/cmd/worker/main.go:250`
  (wakeup listener), the `TableLockManager` wiring, and the notification-trigger install in
  `database.Migrate` (`backend/internal/database/notify.go`, PostgreSQL-only because SQLite has
  no `NOTIFY`). Anything not on that list behaves identically on both editions.
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
| Job wakeup latency | instant on a queued job (`opswakeup`, installed by `database.Migrate`); 5-second poll as the fallback | 5-second poll | **Lite is weaker** |
| Multi-node / horizontal scale | supported | not promised; LiteFS retired (ADR 0001) | **Lite limit** |
| Multi-user data scoping | plain `owner_user_id` filters plus role checks | identical | **Same** |
| Subsonic / OpenSubsonic `/rest/*` | full surface | identical; the only engine-sensitive SQL is `ORDER BY RANDOM()` (`subsonic.go:1378`, `:1439`), which is portable. The whole group is gated by config (`cfg.Subsonic.Enabled`, `cmd/server/main.go:491`), not by edition | **Same** |
| Aggregate stats (`/api/stats/*`, CLI `stats`) | `SUM(CASE WHEN …)` aggregates, deliberately not `FILTER (WHERE …)` | identical, and measured working | **Same** |
| Schema evolution | GORM `AutoMigrate` + `database.Migrate` backfills, portable SQL | identical | **Same** |
| `netrunner-cli` and MCP tools (`cmd/agent`) | the same command and tool set: read-only probes plus stateful writes | identical | **Same** |
| Live job-log / event streaming | console attaches to a chosen job; backlog on connect, then live lines — the `opsevents` trigger fires on every `job_logs` insert, installed by `database.Migrate` | console attaches to a chosen job; backlog on connect, then silence | **Postgres only** |

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
3. **No live push.** PostgreSQL publishes wakeups and log lines now (see the matrix); Lite has no `NOTIFY` and keeps polling.

## Gaps that are not edition differences

Recorded here so the Lite story is not blamed for them, and so nobody "fixes Lite" by chasing them.

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
         rg 'pg_notify'         -> the SQL bootstrap and tests only; no compose mount
         rg 'db/init'           -> docs, codemaps, skills, two test comments; no compose mount
```

The dormant fast path was the first entry on the ADR 0004 follow-up list, so its fix is measured
separately — same host, against the compose `postgres` service (fresh database, so the install path
is the one a new deployment takes):

```
api    : DATABASE_URL=postgres://…@127.0.0.1:5432/… go test ./internal/api \
           -run 'TestAcquireAPIEnqueuePublishesAWakeup|TestJobLogInsertPublishesToTheConsoleStream'
         -> 2 passed: POST /api/acquire published `opswakeup` {event:job_queued, jobtype:acquisition},
            and a `job_logs` insert published `opsevents` {event:job_log, job_id, log_id}
         (both listen on the channels the two consumers subscribe to, and prove the listener was
          live before the write, so neither can pass by losing its own race)
static : rg 'pg_notify'         -> backend/internal/database/notify.go + tests only
```

Note what the install path had to be, and why the bootstrap file could not have been it: `ops/db/init/`
is not in the image at all (`backend/Dockerfile` copies `./backend` and `ops/web`), Docker's init
scripts run against a **fresh** database only, and `02-functions.sql` attaches its triggers to
`joblogs` and `sources` — tables the runtime schema does not have (GORM creates `job_logs` for
`database.JobLog`). Installing from `database.Migrate` reaches a fresh volume and an existing
database alike, on every boot.

## Keeping it true

- A feature that gains an `IsPostgres` branch belongs in the matrix with a verdict.
- The fast path's two halves are held together by
  `backend/internal/api/netrunnerlite_boundary_test.go`: the installer must be called by a boot, and
  every channel it publishes on must be one a listener subscribes to. A listener or a sender that
  moves alone fails there instead of quietly polling forever.
- If Lite's lock manager gains an ownership-checked release or a monotonic `expires_at`, state the
  new guarantee instead of quietly deleting the caveat.

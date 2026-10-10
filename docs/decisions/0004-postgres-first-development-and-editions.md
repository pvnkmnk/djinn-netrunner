# ADR 0004: PostgreSQL-First Development and the Two Editions

## Status

**Accepted in principle by the operator on 2026-10-09.** This record captures direction: which
database we develop and verify against, and that the product ships in two editions. It does not
authorize the mechanical steps listed under *Not yet done* — each of those needs its own
go-ahead. No schema, deployment, credential, or user-visible naming change is authorized here.

## Context

The codebase has carried two backends since v0.0.1, selected by `DATABASE_URL` (`.db` → SQLite,
`postgres://` → PostgreSQL), and SQLite is documented as the *default* in several places: the
README badge (`architecture-standalone-sqlite`) and its "Default persistence layer" line,
`docs/ARCHITECTURE.md`'s "Primary system-of-record", and AGENTS.md's host-run guidance.

SQLite is the weaker backend, and the difference is not cosmetic:

| Capability | PostgreSQL | SQLite |
|---|---|---|
| Job claiming under concurrency | `FOR UPDATE SKIP LOCKED` | atomic status updates, one worker |
| Per-scope exclusion | real `pg_try_advisory_lock` | `TableLockManager` — a `locks` table row, 15-minute TTL, not cross-process-safe |
| Worker wakeup | `LISTEN/NOTIFY` | polling interval |
| Aggregate SQL | `FILTER (WHERE ...)` | portable rewrites only |
| Multi-node | native | LiteFS only (ADR 0001) |

So the paths that only PostgreSQL can exercise — concurrent claims, advisory locking, notify-driven
wakeups — are the ones the development machine tests least, because the development machine runs
the weakest backend. Several defects in this project's history surfaced first on the live
PostgreSQL stack, not in the suite that runs on SQLite.

The operator's decision: **develop against PostgreSQL.** SQLite stays supported, but as a
deliberately reduced-capability edition rather than the thing we optimize for.

## Decision

### 1. PostgreSQL is the development and verification target

A change is considered verified against the database it is developed on. `go test` may keep
running on SQLite for speed, but "green locally" no longer means "verified" for anything touching
claims, locks, wakeups, or aggregate SQL: those are checked against PostgreSQL before the change
is called done.

### 2. SQLite becomes an edition, not the default

| | **Djinn-Netrunner** (full) | **NetrunnerLite** (reduced) |
|---|---|---|
| Database | PostgreSQL | SQLite file |
| Workers | multiple, real advisory locks | one (`MaxConcurrentJobs > 1` warns) |
| Wakeups | `LISTEN/NOTIFY` | polling interval |
| Multi-node | supported | not promised |
| Feature set | everything | a documented subset, audited per feature (§4 below) |

The driver detection and both code paths stay in one codebase; the editions differ by
configuration and by what each one promises, not by a fork.

### 3. Naming

- **Djinn-Netrunner** — the full suite. "Djinn" is the genie that grants the wishes: every magic
  music tool, and the hardware to run them all.
- **NetrunnerLite** — the reduced edition: the genie's assistant or apprentice, capable on its own
  and honest about the wishes it cannot grant.

The genie/apprentice framing is **release-copy material**: landing page, announcement, README
intro. Park it here for that purpose. It is not architecture vocabulary — do not use it to explain
a subsystem, and do not spend code or doc identifiers on it. Product documentation keeps
describing behaviour, not metaphor.

### 4. What we stop doing

- Stop describing SQLite as the default where the claim is about what *we* develop and verify
  against. It can still be described as the no-dependency deployment option.
- Stop accepting "it works on SQLite" as evidence for a concurrency-sensitive change.
- Do not add new capability that only works on SQLite.

### 5. What this record does NOT authorize

- Removing the SQLite driver, its schema path, or its tests.
- Changing `DATABASE_URL` values, `.env`, the compose files, or any credential.
- Retiring ADR 0001 (LiteFS); it stays on record, simply outside NetrunnerLite's promise.
- Renaming images, packages, routes, or UI strings. Naming lands with the release copy.

## Rollout status

1. **A host-reachable PostgreSQL for development — done 2026-10-09.**
   `docker compose up -d postgres` in the `djinn-netrunner` compose project created
   `netrunner-postgres` (PostgreSQL 16.15, healthy) publishing
   `127.0.0.1:${PG_HOST_PORT:-5432}:5432`, and reused the existing
   `djinn-netrunner_netrunner-postgres-data` volume — 21 tables, prior dev data intact. Before
   this, nothing on the host listened on 5432/15432/25432: the running stack was the e2e overlay
   and its `e2e-postgres` publishes no host port at all.
2. **The `dev` secret now points at PostgreSQL — done 2026-10-09.** Infisical `dev`'s
   `DATABASE_URL` went from `netrunner.db` (SQLite) to the host form of the compose URL, using the
   **loopback literal `127.0.0.1`** rather than `localhost` — `localhost` resolves to `::1` first
   on this host while the publish is IPv4-only. Containers are unaffected: both compose files
   inject the in-network URL, so this key only affects host runs.
3. **Correct the wording** in README.md, docs/ARCHITECTURE.md, docs/WHITEPAPER.md,
   docs/RUNBOOK.md, and AGENTS.md, from "SQLite is the default" to "PostgreSQL is what we develop
   against; SQLite is the NetrunnerLite edition".
4. **Define NetrunnerLite's boundary** — the capability table in §2 is a direction, not a promise.
   It needs a per-feature audit (watchlists, acquisition pipeline, Subsonic surface, multi-user
   scoping, WebSockets) before it is published as what Lite does and does not do.

## Consequences

The backend we test most becomes the one we ship, and the concurrency/notify paths get exercised
on every change instead of only on release. Lite becomes an honest documented subset rather than
an accident of whichever backend a user happened to configure.

The costs are real: development now depends on a database service being up, SQLite-only paths lose
the incidental coverage they were getting for free, and two editions mean two support surfaces and
eventually two release pipelines.

Existing SQLite installations keep working. Nothing here changes runtime behaviour on its own.

## Verification and review

This decision is reviewable without code. It holds if: (a) AGENTS.md and the README describe
PostgreSQL as the development target and SQLite as the Lite edition; (b) the `dev` secret set can
boot a host-run server against a PostgreSQL instance; (c) nothing in the repository promises Lite
capability the table in §2 denies. Revisit when items 3–4 land, and put the naming into the release
copy, not the docs.

Measured for item 2 (2026-10-09): a host-run server started with `infisical run --env dev`
answered `200` from `/api/health` with `database: ok`; its connection appeared in
`pg_stat_activity` from the host address (`172.23.0.1`); the boot log contained no SQLite
`PRAGMA` statement; and no `netrunner.db` appeared in the checkout. Those four checks are what
"development runs on Postgres" means in practice — re-run them after any change to the dev
database.

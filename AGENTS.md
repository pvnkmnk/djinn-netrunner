# Agents Guide — NetRunner

> Condensed 2026-09-18 (reference prose compressed; every lesson kept).

## Orientation

NetRunner is a Go music-acquisition and library-operations platform: watchlist
ingest (Spotify, Last.fm, ListenBrainz, RSS, local), acquisition jobs through
slskd (Soulseek), metadata enrichment, local libraries, Fiber + HTMX UI.

- **Before any task, read `codemap.md`** (project root) for architecture,
  entry points, and data flow; for deep work also read the folder's own
  `codemap.md` (e.g. `backend/internal/services/codemap.md`).
- **Autonomy:** read-write for local code/docs/tests/non-destructive tooling;
  runtime/deployment changes (compose, prod env, credentials) are
  operator-reviewed.
- **Auth is session-cookie based** (`session_id`, roles `user`/`admin`
  checked at handler/service boundaries) — NOT the older JWT-first design.
  This is the source of truth for API behavior.
- Entry points: `backend/cmd/{server,worker,cli,agent}`; layers:
  `internal/api` (Fiber handlers), `internal/services` (logic),
  `internal/database` (GORM models/migrations), `internal/agent`
  (transport-agnostic facade for MCP+CLI), `internal/interfaces`
  (`WatchlistProvider`, `SpotifyClientProvider`), `internal/integration`
  (tagged tests), `internal/testutil` (test doubles). Ops in `ops/`
  (compose, `caddy/` reverse proxy, `db/` init+migrations, `web/` Pongo2
  templates + static JS); `e2e/` Playwright; `scripts/` helpers;
  `.github/workflows/` (Go CI + coverage, Docker, E2E, PRGuard/PR-Sentry);
  `conductor/` removed (content in Linear).

## Setup & commands

```bash
cp .env.example .env        # DATABASE_URL, SLSKD_API_KEY; JWT_SECRET recommended
cd backend && go mod download
cd backend && go vet ./...  # and before every PR
cd backend && go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent   # core suite
cd backend && go test ./... # full non-tagged suite
cd backend && go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent
cd backend && go run ./cmd/server   # auto-runs migrations; worker/agent/cli likewise
docker compose up -d                # full stack; logs: docker compose logs -f netrunner[-slskd]
./scripts/integration-tests.sh test # or, from backend/: go test ./internal/integration/... -tags=integration -v
./scripts/smoke-test.sh             # deploy + health/auth/CRUD checks
./scripts/validate.sh            # (Windows PowerShell: ./scripts/validate.ps1)
govulncheck ./...                   # CI fails on reachable CVEs
```

Key env vars (full list in `.env.example`): `DATABASE_URL` (postgres:// or
SQLite path), `SLSKD_API_KEY` (required for acquisition), `JWT_SECRET`
(set explicitly for stable restarts), `MUSIC_LIBRARY` (default
`./music_library`), `GONIC_USER`/`GONIC_PASS` (conditional), docker-only
`POSTGRES_PASSWORD`, `SLSKD_USERNAME`/`SLSKD_PASSWORD`; test-only
`SKIP_INTEGRATION_TESTS`, `SKIP_NETWORK_TESTS`; integration vars
(`INTEGRATION_*`, `SLSKD_TEST_*`) per `scripts/integration-tests.sh`.

## E2E (Playwright)

Entry point: `bash scripts/e2e.sh test` from the repo root (or `bash ../scripts/e2e.sh test` from `e2e/`) — **Playwright owns the
stack lifecycle**: `webServer` runs `e2e/setup-test-db.sh`
(build/create-DB/start/seed, `.env.e2e` materialised from the checked-in
`.env.e2e.example`), `globalTeardown` tears down only when `CI=true`.
Full run ~5 min / ~266 tests (18 skip). `reuseExistingServer: !process.env.CI` — local
runs reuse the stack, so single-spec iteration is ~4s instead of ~4min.

- **After backend/frontend changes, rebuild the stack first** — it runs
  compiled binaries, not live source:
  `docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml up -d --build`.
- **CRITICAL: run `npx playwright` from `e2e/`** — from repo root it resolves
  to a global playwright (version mismatch) with the cryptic error
  `"test.describe() called from async test.describe() block"`.
- **CRITICAL: `getCsrfToken()` is async — `await` it.** An un-awaited call
  yields `"[object Promise]"` and every request 403s (DJI-441: 29 missed
  awaits in `auth.spec.ts`). The #1 cause of auth E2E failures.
- Auth fixtures (`authenticatedPage`, `adminPage`) login via `/api/auth/*`;
  POSTs need the CSRF token from the `csrf_` cookie; the e2e overlay raises
  the rate limit to 1000 req/min.
- **A 403 on a POST may be the CSRF gate, not the behavior under test** — and
  it can make wrong assertions pass vacuously (a cross-owner probe "passed" on
  a CSRF-403). Send the cookie's token on every deliberate mutating request so
  the asserted status is specific (DJI-434).
- htmx pushes the URL with the swap: assert the pushed URL with
  `await page.waitForURL(...)` — `page.url()` right after `waitForResponse`
  races the push and sees the pre-swap URL (DJI-499).
- Templates are baked into the e2e image, not bind-mounted — a browser-level
  mutation check needs an image rebuild; pin page/partial contracts in Go
  handler tests and use Playwright for the end-to-end proof.
- The fixture's admin promotion (`docker exec e2e-postgres psql ...` via
  execFileSync resolving `DOCKER_BIN` → PATH → Docker Desktop default) works
  on Windows shells since #252 — the earlier bare-`docker` PATH reliance
  only worked where docker was already resolvable.
- **Live worker-driven specs need the e2e worker wired to the test DB** —
  the e2e overlay's `ops-worker` overrides `DATABASE_URL` to `musicops_test`
  (base file points it at `musicops`, which nothing creates) and sets
  `YTDLP_PROXY`. `egress-refusal.spec.ts` is the first worker-exercising spec.
- **The Job model has no json tags** — `/api/jobs/` returns `"ID"`/`"State"`;
  poll specs must read both casings or terminal-state detection never fires.
- Acquisition jobs need a **unique `scope_type:scope_id`**: the advisory lock
  key is a hash of that pair, so every empty-scope job contends on one key —
  a leaked lock requeues them forever ("Scope locked, requeueing") and an
  older stuck job wins `ORDER BY requested_at` and starves newer seeds.
  Production jobs are always scoped; seed helpers must be too.
- An acquisition item's full failed lifecycle is ~5 min (max_attempts ×
  retry backoff); a spec asserting terminal state needs a ≥7 min deadline and
  must break ONLY on the terminal state — breaking on a log line races the
  retry scheduler. Also note `example.com` is NXDOMAIN on this home network's
  DNS filter; `httpbin.org` resolves.
- **The egress boundary (DJI-501)**: worker `YTDLP_PROXY` → squid sidecar
  (`ops/squid/`); healthcheck is `bash -c '< /dev/tcp/127.0.0.1/3128'` (the
  image has no `squidclient` and dash lacks `/dev/tcp` — the healthcheck
  string must invoke bash explicitly). A refusal probe must assert
  proxy-specific markers (`Tunnel connection failed|ProxyError`), never the
  URL — the attempt line contains the URL, which makes the assertion
  vacuous. Local runs seed probes via the gated
  `POST /api/test/seed-fallback-refusal`; clean probe rows between runs (the
  spec does not delete them).
- The `reuseExistingServer: !CI` shortcut hides stale code: a rebuilt seed
  endpoint never reaches a running stack (Playwright skips setup when healthy).
  After backend/template changes, `docker compose --env-file ../.env.e2e -f
  ../docker-compose.yml -f ../docker-compose.e2e.yml up -d --build ops-web
  ops-worker` (from `e2e/`) before trusting a run against new code.
- **GA-gap probes** (`e2e/tests/ga-probes.spec.ts`, PR #259) drive three live
  clauses: Soulseek-entrance wrong-work refusal, success path → library, and
  multi-hop post-handover refusal. `POST /api/test/seed-*` endpoints (gated
  `E2E_ENABLE_TEST_API`) create the items and accept `max_attempts` so a
  deterministic probe skips the ~5-min retry ladder.
- **`ops/fake-slskd`** replaces slskd in the e2e overlay — keep the container
  name `netrunner-slskd` (overlay renames break `SLSKD_URL`'s DNS resolution).
  It re-stages its peer file **at enqueue time**: the pipeline's terminal
  discard deletes staged bytes, so startup-only staging leaves later runs
  stat-failing. Fixture FLACs carry a unique comment tag (deterministic ffmpeg
  bytes hit the hash-duplicate path on rerun); a decoy must disagree with the
  request on **both** artist and album axes — one equal axis reads as
  "duplicate recording", not refusal.
- The multi-hop flip endpoint is **request-shaped, not a counter**: the
  guard's pre-handover walk sends `Range: bytes=0-0` (yt-dlp sends none), so
  answer that with 200 and everything else with 302→RFC1918. A counter gets
  consumed by attempt 1 and attempt 2's walk records the *pre-flight* refusal
  — the wrong layer, catchable only by asserting the wording.
- `scripts/mutation-check.sh <gate|boundary>` automates mutation proofs:
  snapshot the file, mutate, expect spec FAIL, restore **from the snapshot,
  not `git checkout --`** (the latter wipes unrelated uncommitted work), then
  require a passing control run. Route Playwright output to a file — exit
  codes through `tail` pipelines see tail's status, not playwright's.
- Automated mutation proofs cover the **identity gate**
  (`scripts/mutation-check.sh gate` — mutates `download_gate.go`; the
  Soulseek wrong-work probe must fail) and the **egress boundary**
  (`boundary` — removes `YTDLP_PROXY`; the multi-hop probe must fail),
  re-proven weekly by `.github/workflows/mutation.yml`. Browser behavior
  mutations (remove admin gate) still need an image rebuild + spec run
  (templates are baked into the image) — see Linear DJI-434.
- **A workflow that has never run will fail on its first scheduled fire.**
  Dispatch `workflow_dispatch`-able workflows once right after merge: the
  mutation workflow needed three runner-only fixes before its first green
  run (2026-09-19, run 35471332835), none reproducible locally.
- **Actions sets `CI=true` ambiently**: Playwright invocations that pass
  locally flip `reuseExistingServer` to false on a runner and refuse the
  already-running stack ("port 8080 already used"). A harness sharing one
  long-lived stack across phases must strip CI for its probe runs
  (`env -u CI npx playwright ...`).
- **`compose up -d --build` may not recreate a container**: a rebuild that
  lands on the same image id (layer-cache hit) prints "Built" but leaves
  the old container "Running" — a "mutated" build silently serves the
  stale binary. Mutation phases need `compose build --no-cache` +
  `up -d --force-recreate` (proven by run 35470657152's false pass).

## API & data contracts (non-obvious)

- HTTP API is HTMX-first: `POST /api/auth/login` answers **302 + Set-Cookie
  with an empty body**; the `csrf_` cookie is minted by *any* request (even a
  404 like `GET /login`) — refresh it with BOTH `-b` and `-c` or the session
  cookie gets wiped. `csrf_` must NOT be `httpOnly` (the double-submit
  pattern reads `document.cookie` and echoes it as `X-CSRF-Token`); the
  *session* cookie is the httpOnly one.
- **JSON responses use PascalCase fields** (`ID`, `Name`, `SourceType`) —
  E2E must check `response.ID`, not `response.id`.
- Public: `/api/health` (no auth), `/api/auth/{register,login,logout}`
  (rate-limited). Pages under `/`, protected pages and `/partials/*` (HTMX)
  and `/api/*` CRUD groups require a session; profile writes are admin-only.
  `GET /api/health` reports `database` unconditionally; `slskd` needs an API
  key, `disk` needs the library path, `gonic`/`navidrome` need their URL —
  assert the contract (every reported key carries a `status`), not a fixed
  key set. WebSocket: `/ws/events`, `/ws/jobs/:job_id`. Subsonic `/rest/*`
  requires the `.view` suffix. Route assertions should read
  `app.GetRoutes()` (filter by method — middleware shows as `USE`).
- CLI (`netrunner-cli`): `status`, `config list`, `watchlist
  list|add|sync|import`, `library list|add|scan|prune|rm`, `profile
  list|add|rm|set-default`, `stats summary|jobs|library`.
- MCP tools (`backend/cmd/agent`, stable v0.0.1): 20 tools — read-only
  probes (`probe_system`, `read_config`, `list_*`, `get_stats`,
  `search_library`, `get_job_logs`) and stateful ones (`update_config`,
  `add_watchlist`, `sync_watchlist`, `enqueue_acquisition`, `bootstrap`
  (runs AutoMigrate, safe to retry), `scan_library`, `add_library`,
  `cancel_job`, `retry_job`). All return `mcp.CallToolResultError` on
  failure; read-only tools leave no partial state, write tools may leave a
  partially created row if a follow-up step fails.
- Models (GORM, `backend/internal/database/models.go`): `User`(1:N
  `Session`), `QualityProfile`, `Watchlist`, `Schedule`, `Job`(1:N
  `JobItem`,`JobLog`), `Acquisition`, `Library`(1:N `Track`),
  `MonitoredArtist`(1:N `TrackedRelease`), `MetadataCache`, `SpotifyToken`,
  `Lock`, `Setting`, `PeerReputation`. Schema sources:
  `ops/db/init/01-schema.sql` + `02-functions.sql` + `migrations/*.sql`;
  runtime `database.Migrate(db)` + AutoMigrate.
- `interfaces.WatchlistProvider`:
  `FetchTracks(ctx, watchlist) ([]map[string]string, string, error)`,
  `ValidateConfig(config string) error`;
  `interfaces.SpotifyClientProvider`: `GetClient(ctx, userID)`.

## Database drivers

Driver auto-detects from `DATABASE_URL` (`.db` → SQLite WAL, `postgres://`
→ Postgres). **SQLite is single-writer only**: no `pg_try_advisory_lock`
(`TableLockManager` emulates via a `locks` table inside a transaction —
locks expire after 15 min, not cross-process-safe), no `FILTER (WHERE ...)`
(agent stats queries fail), GORM AutoMigrate only, `LiteFSGuard` checks
`/litefs/.primary`. Postgres: pooling, real session-level advisory locks,
`LISTEN/NOTIFY` job wakeup, SQL bootstrap + AutoMigrate. The
`WorkerOrchestrator` takes advisory locks per scope ID before processing;
if `MaxConcurrentJobs > 1` with SQLite the worker warns at startup — use
Postgres for concurrent production workloads.

## Common tasks

1. **New feature:** locate the layer (api/services/database), minimal
   handler+service+model changes, extend `*_test.go` in that package, then
   `go vet ./...` + `go test ./...`.
2. **Bug fix:** reproduce with a targeted test
   (`go test ./<pkg> -run <Test> -v`), trace handler→service→database,
   smallest safe patch + regression test, re-run package then broader suite.
3. **Migration/schema change:** update `models.go`; SQL-specific
   transformations go in `ops/db/init/migrations/`; ensure `database.Migrate`
   handles the transition; validate both driver paths when available.
4. **Dependency update:** `go get`, `go mod tidy`, then vet/test/build +
   `govulncheck`.
5. **Deploy/full stack:** set env, `docker compose up -d --build`, check
   `curl localhost:8080/api/health`, follow logs.

## Pitfalls & Gotchas

- Prior-guide corrections: auth is session-cookie (not JWT+RBAC); rate
  limiting uses Fiber limiter defaults (no Redis); reverse proxy is Caddy
  (`ops/caddy/Caddyfile`), not Nginx; explicit `"admin"` role checks exist —
  keep consistent unless doing a coordinated RBAC refactor.
- `database.Migrate` contains PostgreSQL enum-to-text conversions; read it
  before modifying job state columns.
- `backend/entrypoint.sh` is a single-process bootstrap (dirs, logs, exec);
  compose runs `ops-web`/`ops-worker` as separate services with `command`
  overrides. Debug locally with `go run ./cmd/server|worker`.
- Spotify OAuth defaults the callback to
  `http://localhost:8080/api/auth/spotify/callback` unless
  `SPOTIFY_REDIRECT_URI` is set.
- **Pongo2 renders Go bools as `True`/`False`** (capitalized) — use
  `{% if field %}true{% else %}false{% endif %}` for lowercase (broke E2E on
  `Lossless: True`).
- **Pongo2 `{% if ID %}` is always true for UUIDs** (zero UUID is a
  non-empty string) — pass an explicit `IsNew` bool to distinguish add/edit.
- **`encoding/json` silently drops fields without JSON tags** — `source_uri`
  ≠ `SourceURI` (case-insensitive fallback doesn't cover underscores); both
  model AND input struct need tags (DJI-437).
- **HTMX only swaps 2xx responses** — 4xx/5xx error paths silently no-op;
  check `isHTMXRequest(c)` and return an error partial via
  `c.SendString(...)` (DJI-438).
- **Create handlers must set `HX-Trigger: closeModal`** before returning the
  partial, or the modal never closes (only `AcquireHandler.Create` does it;
  DJI-440).
- **Pongo2 `{# #}` comments cannot span lines** — keep template comments
  single-line.

## Consolidated workspace learnings (merged from DevWorks base, 2026-09-18)

> Repo-specific operational scar tissue. Where an entry overlaps a section
> above it adds nuance rather than replacing it.

### GitHub, CI & review workflow

- Default branch is `master`, not `main`.
- Merge PRs with `gh pr merge N --repo pvnkmnk/djinn-netrunner --squash
  --delete-branch`. It often prints **nothing** on success (exit 0, empty
  stdout) — confirm with `gh pr view N --json state,mergeCommit` instead of
  inferring failure from silence. `git reset --hard origin/master` after is
  safe only because master only fast-forwards — check
  `git reflog show master` before assuming that on a shared checkout.
- The `integration` CI job can fail in ~26s with `connection reset by peer`
  pulling Navidrome from Docker Hub — a registry flake that hits docs-only
  commits too. Re-run the workflow instead of debugging the diff.
- `internal/integration/*_test.go` sits behind `//go:build integration`, so
  `go test ./...` never compiles it: changing a shared signature passes
  locally and fails CI. Run `go vet -tags integration ./...` (or
  `go test -tags integration -run XXNONE ./internal/integration/...`) before
  pushing.
- `gh workflow run e2e.yml --ref <feature-branch>` works even though the
  workflow watches only `master`: `workflow_dispatch` needs the workflow on
  the *default* branch, not the dispatched ref — how to prove workflow-only
  changes without merging first.
- When a bot leaves a review thread open and won't flip it (`@coderabbitai
  resolve` replies can lag), resolve directly via GraphQL: get the thread id
  from `reviewThreads`, then `gh api graphql -f query='mutation {
  resolveReviewThread(input: {threadId: "PRRT_..."}) { thread { isResolved } }
  }'`.

### Go toolchain & dependencies

- CI pins `go 1.25.13` (setup-go) and the Docker builder uses
  `GOTOOLCHAIN=local`; local Go is newer. Never let `go get`/`go mod tidy`
  bump the `go` directive in `backend/go.mod` — check it LAST, after tidy
  (any module requiring go ≥ 1.26 forces it back up, e.g. x/sync).
- The `test` workflow runs govulncheck and fails CI on reachable CVEs.
  Downgrading transitive deps to appease the directive reintroduces CVEs —
  keep master's dep versions and add new libs at go-1.25-compatible
  releases.
- Runner Go installs can be transiently corrupted (`compile: version X does
  not match go tool version Y` in stdlib internals unrelated to your diff) —
  re-run before debugging.

### Build, test & integration

- **Line endings are mixed per-file in this repo**: most files are CRLF but
  `acquisition_pipeline.go` (among others) is LF. Detect the dominant ending
  before patch-scripting and preserve it — forcing CRLF onto an LF file
  corrupts it (CRCRLF), and str_replace anchors written with the wrong ending
  silently match nothing.
- `py -c "print(...)"` with emoji/box-drawing output dies with cp1252
  `UnicodeEncodeError` (MSYS pipes it fine) — start such scripts with
  `sys.stdout.reconfigure(encoding='utf-8', errors='replace')`. Same for
  reading JSON from `gh api` (codebot names contain emoji): use
  `io.open(..., encoding='utf-8')`, never the cp1252 default.
- **`write_file` can report success while the file never lands** (seen twice:
  `backend/_patch/dji501_fix_overlay.py`). After a write that later steps
  depend on, `ls` the path before running it — and prefer direct
  `str_replace` edits for one-off fixes when a script keeps failing to write.
- `PORT=0` is exported by this environment, making
  `go test ./internal/config` fail (`TestLoad_Defaults: cfg.Port = "0", want
  "8080"`). Run Go tests as `PORT= go test ...` — environmental, not a repo
  bug; don't "fix" config.go.
- The scanner indexes with a 4-goroutine pool, so a `:memory:` SQLite test
  DB gives each pooled connection its own database ("no such table"). Use a
  file-backed DSN under `t.TempDir()` closed via `t.Cleanup` — Windows
  refuses to remove the directory while the DB file is open.
- `cmd/cli` tests swap package globals (`db`, `cfg`, `jsonOutput`, `osExit`).
  Stub `osExit` before any test that can reach `handleError`, or the real
  `os.Exit` kills the test binary mid-run. `setupTestDB`'s `:memory:` is
  unsafe there for the same pooled-connection reason — file-backed DSN under
  `t.TempDir()`.
- `acquisitionPipeline` test fixtures must set `ctx: context.Background()`:
  the probe stage calls `context.WithTimeout(p.ctx, ...)`, which **panics**
  (`context.WithDeadlineCause`) on a nil parent — the failure surfaces as a
  crashed stage, not an assertion.
- ffmpeg/ffprobe are mise-installed; several tests skip without them
  (`requireProbeTools`). Two PATH layers needed: the WinGet Links dir (so
  mise *shims* can find mise itself — without it every shim dies with
  `mise-shim: failed to execute mise: program not found`) and
  `.exe`-suffixed shim copies in a PATH dir (Go's `exec.LookPath` only
  accepts PATHEXT extensions). Recipe:
  `mkdir -p ~/.ffbin && cp .../mise/shims/ffmpeg ~/.ffbin/ffmpeg.exe && cp .../mise/shims/ffprobe ~/.ffbin/ffprobe.exe`
  then
  `PATH="/c/Users/idols/AppData/Local/Microsoft/WinGet/Links:$HOME/.ffbin:$PATH"`.
  ffmpeg 9.0.1.
- Full `internal/services` suite takes ~65–95s (longer when ffmpeg is
  reachable and probe tests stop skipping).
- Integration suite: bring up `docker-compose.integration.yml` (services
  `netrunner-postgres/navidrome/slskd-integration/app`, app on port 18080),
  run with `INTEGRATION_TESTS=1 INTEGRATION_BASE_URL=http://localhost:18080`.
  Without the env vars, tests are excluded by build tags or fail with
  misleading connection-refused.
- Integration mock slskd contract: download polls must return state
  `"Completed, Succeeded"` (exact string — `IsSucceeded` requires it) and
  search `"state":"Completed"`, else waits burn full budgets (30s search,
  75s download). This exact quirk once caused a pipeline E2E skip.
- Integration smoke tests (`TestSmoke_Library_CRUD`, `TestSmoke_Quota_Warning`)
  delete the library at `/app/music` first — leftover demo libraries there
  fail with `update or delete on table "libraries" violates foreign key
  constraint "fk_tracks_library"`. Delete those rows (`DELETE FROM tracks
  WHERE library_id IN (SELECT id FROM libraries WHERE path='/app/music')`,
  then the library) before suspecting a regression.
- Libraries and monitored artists are owner-scoped (`owner_user_id`): a
  fixture from an earlier session's user is invisible to a new session's API
  calls (`{"error":"artist not found"}`, empty Subsonic `getIndexes`) —
  re-point ownership (`update monitored_artists set owner_user_id=<id>`)
  instead of recreating fixtures. Library creation at an existing path is
  idempotent for the same owner (200, same id) and 409 + HTMX error partial
  for another owner's path (used to be a bare 500).
- Route-table assertions read `app.GetRoutes()` (method + full path), not
  endpoint probes; group middleware appears as a `USE` route, so filter by
  method before asserting no endpoint exists under a prefix. Subsonic's
  `.view` suffix deserves assertion across the whole `/rest` family — a
  violation surfaces as `Cannot GET` (missing-route message), not a naming
  error.

### Acquisition pipeline & library scanning

- `Track.Path` is `not null` + `uniqueIndex`: a scanner that forgets it
  inserts one `path=''` row then fails every later file on `idx_tracks_path`
  while the job still reports "Completed" — a scan of N files silently
  yields 1 track and nothing can stream. `/api/libraries/:id/tracks` nests a
  zero-valued `library` object, so grepping that response for `"path":""`
  false-positives; match on the library path prefix instead.
- The worker dispatches on `job_type` and fails unknown types with
  `unsupported job type: <x>`. It runs **several jobs concurrently** (three
  acquisition jobs observed `running` at once under one `worker_id`) — a
  stalled item blocks only its own job. `WatchlistHandler.SyncWatchlist`
  must create type `sync` (not `watchlist_sync`) with `ScopeType:
  "watchlist"` — exactly what `SyncHandler.Execute` requires.
- The acquisition library client is optional and arrives as a
  `SubsonicClientInterface`. A typed-nil `*SubsonicClient` is NOT a nil
  interface, so `stageCheckLibraryIndex`'s `library == nil` guard passes and
  `Search3` panics — pass a genuine nil interface from the worker AND keep
  the nil-receiver guard in `doRequest`.
- A peer that answers but never sends (`Queued, Remotely`) is abandoned
  after `remoteQueueGrace` (45s) — but only when another candidate remains;
  with `DownloadWaitOptions.HasAlternatives=false` (last candidate) the full
  timeout applies. `CancelDownload` is
  `DELETE /api/v0/transfers/downloads/{user}/{id}`; slskd answers **204 No
  Content** even for unknown ids (never 404) — 200/204/404 all mean "gone".
- Downloaded bytes are validated by `AudioProbe` (ffprobe) and it **fails
  open**: missing/unstartable binary → `ErrProbeUnavailable`, file imported
  with a warning. Only `*exec.ExitError` means ffprobe actually judged the
  file; a cancelled *caller* context is deliberately not a verdict — reading
  either as "unplayable" deletes valid audio. Validation runs per candidate
  *inside* the download loop, so one truncated file is just another failure
  reason and the next peer can still satisfy the item.
- Scanning a SQL column into a GORM field named `Name` reads the `name`
  column, not the aliased one: `SELECT DISTINCT artist` into it yields empty
  strings (the cause of DJI-487's nameless Subsonic artists). Alias the
  column to `name` or tag the field.
- Job cancellation is cooperative: `POST /api/jobs/:id/cancel` only flips
  the row; the worker notices on its next round-robin tick. An idle job
  finalizes through a different path than a running one; a job whose worker
  died (container rebuild) sits `running` with no `finished_at` until the
  janitor stamps it.
- `cleanupEmptyStagingDirs` compared an **absolute** `stagingRoot` against a
  possibly-relative `dir`; `filepath.Rel` errors on the mixed pair, so the
  sweep silently never ran under the default relative `./downloads` staging
  path (it only worked with absolute paths — hence "fine" in Docker and
  `t.TempDir()` tests). Both sides must be made absolute first.
- The MusicBrainz recording-ID dedup branch (3.5 of `importFile`) and album
  branch (3.6) both go through `discardStagedDownload`, which removes the
  file then sweeps upward, stopping at the first directory still holding
  entries (whole-album downloads are many items sharing one folder — an
  eager sweep would delete a sibling another item still needs).
- `importFile`'s error return reaches `failItem` via `ProcessItem` →
  `ExecuteItem` — returning an error IS the correct fail-and-retry path. A
  dedup branch that ignores its `Updates(...)` error and returns `nil` is
  worse than a failure: the worker reports success while the row stays
  `running`, so nothing re-claims it and no retry is scheduled.
- Identity lookup must distinguish "not found" from "query failed":
  `gorm.ErrRecordNotFound` = genuinely new (fall back to filesystem, then
  tag casing); any other error must abort via `ErrIdentityLookup` — falling
  back on a failed query can point a repair at the wrong destination folder.
  Same shape for `findExistingAlbumAcquisition`, which returns `(nil, nil)`
  for a missing row so a caller's `err == nil` cannot read a DB failure as
  "not a duplicate".
- An `acquisitions` row stores the **track** artist, not the album artist,
  so release-group dedup keyed on the stored value never matches a
  multi-credit track against its own album (`Every Time I Die & Daryl
  Palumbo` vs `Every Time I Die`). Widen the lookup to accept either; don't
  change what is stored — that column is display/provenance.
- Artist/album casing is canonicalised by `canonical_identity.go` (case +
  whitespace folded), the single owner for both the dedup key and
  `GenerateLibraryPath`, so they cannot disagree. Resolution order: earliest
  `acquisitions` row wins, then an existing on-disk folder, then the tag
  verbatim — artist resolved *before* album so a new album lands in the
  artist's existing folder. Later-wins would drift as folders come and go.
- `detectAlbumFragments` groups by folded **album** name only, so
  `Band A/Greatest Hits` and `Band B/GREATEST HITS` land in one group; a
  bare case check accepts it and `--apply` then moves one band's track under
  the other's canonical artist. A case-only album match must also require
  artist agreement (`len(artists) == 1 || allCaseEqual(artists)`).
- Merging fragmented folders must carry **sidecar files** (`.lrc`, images)
  with the audio: moving only the `.mp3` leaves the source dir populated and
  never reclaimed (`dirs removed: 0`). A track's own file is excluded from
  its sidecar set by suffix match — `.mp3` shares the stem.
- Standing casing repro in the beta library: `music/PUP/The Unraveling Of
  Puptheband` beside `music/PUP/The Unraveling of Puptheband` (`acquisitions`
  id 4 = lowercase `of`, id 9 = capital `Of`). Use it for identity/path work
  — don't rebuild a fixture.
- Reading live job state: `jobs` uses `job_type` (not `type`), items live in
  `jobitems` (`track_title`, not `title`), `monitored_artists` uses `name`
  (not `artist_name`). No `LOG_LEVEL` config and slog defaults to INFO, so
  `DEBUG` job logs never reach `docker logs` and cannot serve as evidence.

### Media tagging architecture

- M4A/OGG tag writes shell out to ffmpeg (`FFmpegTagger`), NOT a Go library:
  audiometa v1.3.1 panics on real-world MP4 `covr` atoms, and audiometa v3
  (or any dep needing go 1.26) cascades the toolchain. Reads use
  `dhowden/tag` (pure Go).
- ffmpeg recipes: tag writes = re-mux with `-c copy` (lossless); source tags
  propagate (no `-map_metadata -1`); M4A cover = image as 2nd input +
  `-disposition:v attached_pic`; OGG cover = base64
  `METADATA_BLOCK_PICTURE` Vorbis comment (ogg muxer cannot carry a picture
  stream; build the block with `flacpicture`, encode `block.Data` only, not
  the struct). All writes go through unique temp file + atomic rename with
  permissions preserved.
- The runtime Docker image ships ffmpeg (`apk add ffmpeg`) —
  TranscoderService assumed it before it existed; keep it there. It also
  installs `/usr/bin/ffprobe` (verified 6.1.2 in the image), which the
  acquisition download validator calls — no separate package needed, but
  dropping ffmpeg silently disables validation.

### Ops / compose & runtime environment

- Prod compose runs slskd as `user: "1000:1000"` with a one-shot
  `volume-init` chown bootstrap; slskd's entrypoint exits in non-root mode
  if `/app` is unwritable on fresh root-owned volumes. Don't remove
  volume-init. Leftover ad-hoc containers from live debugging (e.g.
  `netrunner-etid-worker2`) are harmless — not part of compose.
- `.env` only drives `${VAR}` substitution unless a service declares
  `env_file:`. Both app services now do (`- path: .env / required: false`)
  and `environment:` still wins, so compose-derived values stay
  authoritative. Symptom of a missing passthrough: `JWT_SECRET not set —
  generated random secret` and sessions dying every restart. Verify with
  `docker compose exec ops-web env`.
- Duplicate keys in an env template take the **last** occurrence silently:
  `.env.beta.example` declared `NAVIDROME_ADMIN_PASSWORD` twice, so the
  placeholder below overrode the value a user set above it.
- Profile-gated services (beta overlay: Caddy behind `edge`, Navidrome
  behind `media-server`) disappear from `docker compose config` output
  entirely — correct, not a failed merge.
- `docker compose --env-file X` replaces `.env` for `${VAR}` *substitution*
  only; a service declaring `env_file: .env` still receives that file's
  values *inside the container*. `docker compose config` can print two
  different values for the same key and only one reaches the process —
  check which block a value came from before debugging.
- One secret, one source: `docker-compose.e2e.yml` once hardcoded
  `musicops:testpass` in `DATABASE_URL` while the postgres role took its
  password from `POSTGRES_PASSWORD` — any `.env.e2e` with a different
  password produced a healthy-looking stack that failed auth. Interpolate
  `${POSTGRES_PASSWORD}` instead.
- `POSTGRES_PASSWORD` applies only on **first** volume init: changing it
  without `docker compose down -v` leaves the role password unchanged —
  teardown must include `-v`.
- The e2e overlay pins `ENVIRONMENT: development` because the base compose's
  `env_file: .env` would otherwise let a developer's local `.env` put the
  throwaway stack in production mode, where a missing `JWT_SECRET` refuses
  to boot; CI has no `.env`, so local and CI would diverge.
- Subsonic auth contract: `u` is the account **email**; `p=` compares the
  bcrypt account password, `t=`/`s=` use the shared `SUBSONIC_PASSWORD`
  (`t=md5(md5(SUBSONIC_PASSWORD)+salt)`). Production refuses to boot with
  Subsonic enabled and no password; token auth is refused when unset —
  otherwise the expected token degenerates to `md5(""+salt)` and anything
  can forge it. A `+` in the email must be sent as `%2B` (a literal `+`
  decodes to a space and the ping fails exactly like a wrong password).
- `ENVIRONMENT=production` hard-fails on missing `JWT_SECRET` (or Subsonic
  enabled without a password); development warns only. Integration/e2e
  stacks run development, so they're unaffected.
- Base `docker-compose.yml` publishes no ops-web port; only the beta overlay
  does (`${BETA_BIND_ADDR:-127.0.0.1}:${BETA_HTTP_PORT:-8080}:8080`) —
  host-side API work (`scripts/beta-smoke.sh`, curl `:8080`) needs
  `docker compose -f docker-compose.yml -f docker-compose.beta.yml up`.
- The live beta stack runs from a *second clone*
  (`projects/beta-bringup/djinn-netrunner`), not the working repo — sync
  changed files there and rebuild `ops-worker` before live verification, or
  you test the previous binary and report it as evidence.
- The runtime image ships only `netrunner-server`/`netrunner-worker` — **no
  `netrunner-cli` inside it**, so documented
  `docker compose exec ops-web netrunner-cli ...` repair steps cannot work.
  Build the CLI for Linux, `docker cp` it in, `chmod +x` as root (the app
  user can't), then run it as the app user.
- `ALLOW_PRIVATE_TARGETS=true` is mandatory on any compose stack:
  slskd/Navidrome/Gonic are reached by service name (a private IP), so the
  SSRF dialer rejects every request with `ssrf: no public IP found for
  <host>`. Provider APIs stay guarded either way. Note the yt-dlp pre-flight
  walk (`checkPublicHost`/`resolveRedirectTarget`) is UNCONDITIONAL — the
  flag only affects the app's service-mesh client, so even in beta a source
  URL that resolves private is refused before the egress proxy is consulted.
- The egress-proxy sidecar logs to `/var/log/squid/` — **never `stdio:/dev/stdout`**:
  squid drops privileges to `proxy` before opening its logs, and root owns
  stdout, which FATALs. Likewise never point `cache_log` at `/dev/null` — it
  blinds squid's own FATAL diagnostics when debugging. Read refusals via
  `docker exec <proxy> tail /var/log/squid/access.log` (`TCP_DENIED/403 CONNECT …`).
- After editing `ops/squid/allowed-domains.txt`, `restart egress-proxy` — a
  plain `up -d` leaves the running squid on its old in-memory allowlist.
- The `ubuntu/squid` image has no `squidclient`; its `/bin/sh` is dash (no
  `/dev/tcp`), but `bash` is present — healthchecks must invoke bash
  explicitly.
- slskd needs its *own* copy of `SLSKD_API_KEY` (the env var only feeds the
  *app* — every search 401s until the slskd service carries it too). Use
  slskd's **primary** key (`SLSKD_API_KEY` / `-k` / `--api-key`,
  documented and stable); it's the `web.authentication.api_keys` *map* whose
  env-var nesting is fragile.
- A primary key shorter than 16 chars makes slskd exit with `API key must be
  between 16 and 255 characters` and **exit code 0** — looks like a clean
  stop, not a crash.
- slskd defaults `directories.downloads` to `~/downloads`, which resolves to
  its *own* `/app/downloads` (slskd data volume), not the volume the worker
  imports from. Without `SLSKD_DOWNLOADS_DIR=/downloads`, transfers report
  `Completed, Succeeded` while the shared volume stays empty and every
  import fails with `Downloaded file not found`.
- `ALLOW_PRIVATE_TARGETS` + a proxy is still buggy in `safe_http.go`: the
  AllowPrivateTargets branch builds a proxied transport then returns a fresh
  `DefaultTransport` clone, silently dropping the proxy.
- Never prefix a whole script invocation with `MSYS_NO_PATHCONV=1`: it also
  disables conversion of the script's own Windows paths, so compose resolved
  `../.env.e2e` to a phantom `C:\c\Users\...` and `e2e.sh down` failed. Scope
  it to the container-side command only.

## Skills & dependency sources

- `.agents/skills/`: `release-readiness-review` (two-phase audit+closure),
  `e2e-test-spec-generator` (Playwright specs from Linear issues),
  `auto-linear-update` (Linear updates from E2E results); see
  `.agents/skills/` for the rest.
- Read-only dependency clones live under `.slim/clonedeps/repos/` for
  inspection (do not edit): `gofiber__fiber` v2.52.13 (middleware chain,
  context, routing), `go-gorm__gorm` v1.31.1 (query building, preloading,
  transactions, migrations), `mark3labs__mcp-go` v0.45.0 (MCP SDK, tool
  definitions, transports).

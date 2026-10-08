# Agents Guide — NetRunner

> Condensed 2026-09-18 (reference prose compressed; every lesson kept).
> Refreshed 2026-10-02: single-escape invariants, guard/mutation discipline,
> Linear state kept in step with merges.

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
  `.github/workflows/` (Go CI + coverage, Docker to GHCR publish, E2E, integration, scheduled
  mutation checks, PRGuard/PR-Sentry);
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
./scripts/deploy.sh [--release] [--profile x]  # documented bring-up; resolves APP_VERSION (git tag, else `dev`) into .env
./scripts/integration-tests.sh test # or, from backend/: go test ./internal/integration/... -tags=integration -v
./scripts/smoke.sh                  # smoke gate vs a running stack (--dev | --release; release is the default, 28 checks)
./scripts/smoke-test.sh             # self-contained: up netrunner-smoke (:18081/:18443/:18444), health/auth/CRUD checks, down
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
`CONFIG_ENV` selects `config.<env>.yaml` over `config.yaml` (multi-environment
YAML config); compose pins `ENVIRONMENT`/`CONFIG_ENV` per deploy path — the
deploy path, not `.env`, decides the mode (dev base pins development, the
release overlay pins production).

## E2E (Playwright)

Entry point: `bash scripts/e2e.sh test` from the repo root (or `bash ../scripts/e2e.sh test` from `e2e/`) — **Playwright owns the
stack lifecycle**: `webServer` runs `e2e/setup-test-db.sh`
(build/create-DB/start/seed, `.env.e2e` materialised from the checked-in
`.env.e2e.example`), `globalTeardown` tears down only when `CI=true`.
Full run ~5 min / a few hundred tests (skip counts move as parked specs get
unparked). `reuseExistingServer: !process.env.CI` — local
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
  a leaked lock requeues them forever ("Scope locked, requeueing").
  Production jobs are always scoped; seed helpers must be too.
- **Claiming is fair as of #541, but contention is still serial per scope.**
  `claimCandidates` filters out queued jobs sharing a running job's scope, and
  `claimAndProcess` walks the batch, so a blocked job can no longer starve newer
  ones. Two queued jobs with the *same* scope are still one-at-a-time by design
  — give a test or seed helper distinct scopes or it will look hung.
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
  spec does not delete them). Seeds accept an optional `peer` spec
  (forwarded to the fake's `/roster` surface), so on-demand peers need no
  fixture-code change.
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
- The e2e test endpoints live in `internal/api/testapi` (mounted only when
  `E2E_ENABLE_TEST_API`); **seeds self-declare their fixture artists to
  cleanup** (request artist + peer TAG artist), so cleanup needs no edits for
  a new clause — and its roster is add-only, never trimmed (DJI-502).
- The e2e overlay tags the slskd stand-in `netrunner/fake-slskd:e2e` — never
  build it under `slskd/slskd:latest` (that shadowed the real image and later
  earlier bring-ups ran a python stand-in as netrunner-slskd, DJI-503).
- Compose publishes postgres as `127.0.0.1:${PG_HOST_PORT:-5432}:5432`;
  host-side client URLs must follow `PG_HOST_PORT` (DJI-504).
- `scripts/mutation-check.sh <gate|boundary|success>` automates mutation proofs:
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
- **A workflow that has never run may fail on its first scheduled fire.**
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

- **`DECLARED_SKIPS` in `scripts/e2e_gate.sh` is exact in BOTH directions** —
  a new skip fails the run, and a declared skip that stops running fails it too
  ("remove them from DECLARED_SKIPS; coverage changed"). A CONDITIONAL
  `test.skip` therefore cannot be expressed at all: assert the failure with a
  message naming the third party instead of skipping on its outage.
- **A local run reuses the stack AND its database**, so rows written through the
  real API survive into the next run — and CI recreates the DB per run, so it
  never sees this class of failure. Owner scoping hides it: a fixture left by
  another session's user reads as *missing* data, not extra, which is the harder
  direction to diagnose. Duplicate rows for one MBID are not rejected per-MBID.

## API & data contracts (non-obvious)

- HTTP API is HTMX-first: `POST /api/auth/login` answers **302 + Set-Cookie
  with an empty body**; the `csrf_` cookie is minted by *any* request (even a
  404 like `GET /login`) — refresh it with BOTH `-b` and `-c` or the session
  cookie gets wiped. `csrf_` must NOT be `httpOnly` (the double-submit
  pattern reads `document.cookie` and echoes it as `X-CSRF-Token`); the
  *session* cookie is the httpOnly one.
- **`api` may import `config`; `config` never imports `api`.** There is no
  import cycle, so `config.BcryptMaxPasswordBytes` and
  `config.MinPasswordLength` are the single source of truth for the password
  bounds and any handler may read them directly instead of re-deriving a
  constant.
- **Playwright's `page.request` follows redirects**, so a handler that answers
  `302 + Set-Cookie` reports `200` and an assertion on the 302 fails against
  correct code. Pass `maxRedirects: 0` to see the real status.
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

**Do not add production machinery to make one test deterministic.** When making a
browser spec hermetic starts needing a cache service, new wiring and new
endpoints, that is scope the task did not ask for: say plainly which assertion
depends on a third party, ship the honest version, and file the seam as its own
ticket. Reviewers unpick the detour first — and unrequested extras inside an
otherwise-good change are the thing to flag before anything else.

## Pitfalls & Gotchas

- **Python's `open(path, "w")` defaults to the Windows locale encoding
  (cp1252), not UTF-8.** Writing an em-dash through it lands the single byte
  `0x97`, and the symptom is a *Go compile error* — `illegal UTF-8 encoding`
  — not a Python `UnicodeEncodeError`. Pass `encoding="utf-8"` explicitly on
  every write; the existing stdout/read rule below does not cover this, because
  reading decodes fine and the damage only appears in the file. Byte-probe with
  `py -c "d=open(f,'rb').read(); print([b for b in d if b>127])"` when a
  patch script rewrites a Go source file containing prose.

- **A `write_file`/heredoc round-trip mangles `"\n"` inside a Python patch
  script**, so the patcher ships with a real newline where an escape was meant
  (or vice versa). Build such literals from `chr(92) + "n"` / `chr(9)`, or
  mutate by LINE INDEX instead of by string anchor — that removes the whole
  class of quoting trap without depending on the writer's escaping.
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
- **Pongo2 truthiness calls a nil pointer true**: it resolves the pointer
  to its zero value, so `{% if field %}` cannot guard a nullable one. A
  zero UUID made Add modals read "Edit" (nil the id out, as
  `libraries.go` does, or pass an explicit `IsNew`); a nil `*time.Time`
  reached the `date` filter and 500'd the whole list (`filter input
  argument must be of type 'time.Time'`) — such fields need a Go-side
  label (`Schedule.NextRunLabel`, `MonitoredArtist.LastScanLabel`).
- **pongo2's autoescape path *is* the `escape` filter** (`variable.go`), and
  `NewPongo2` never disarms it — so `{{ x | escape }}` is a *second* pass
  (`&amp;amp;`) and `|e` has the same effect. `| safe` is the opposite case: it
  *suppresses* autoescaping (`FilterApplied("safe")`), so it is never redundant
  and is the one filter that can turn a database row back into markup — ban it.
  The exemption is `AsSafeValue`, not a Go `template.HTML`, which pongo2 knows
  nothing about. `truncatechars_html`/`truncatewords_html` also return
  `AsSafeValue`, so a ban list must name both spellings.
- **`encoding/json` silently drops fields without JSON tags** — `source_uri`
  ≠ `SourceURI` (case-insensitive fallback doesn't cover underscores); both
  model AND input struct need tags (DJI-437).
- **HTMX only swaps 2xx responses** — 4xx/5xx paths no-op silently unless
  something renders them. `app.js`'s `htmx:responseError` handler shows the
  server's message inside the modal (or the region for a failed load) and
  discriminates on `requestConfig.verb`: the section regions also carry
  `hx-get`, so the target's attributes cannot tell a failed save from a
  failed load (DJI-438).
- **The CSP refuses htmx's own indicator styles.** htmx 1.9.10 injects
  `.htmx-indicator{opacity:0}` as an inline `<style>` block at DOMContentLoaded
  (unless `includeIndicatorStyles: false`). This app sends `style-src 'self'`,
  which refuses inline style *elements*, so htmx's copy never applies and any
  `.htmx-indicator` stays visible forever — the permanent "Searching…" on the
  browse page. The pair is now declared in `styles.css`, which `'self'` allows.
  Same rule for any `style="..."` attribute: it needs `'unsafe-hashes'` or
  `'unsafe-inline'`, so move it to a stylesheet instead of relaxing the policy.
- **A class merely *mentioned* in the CSS is not a styled class.** After `3734d90`
  the only surviving `.btn` in `styles.css` was inside a `:focus-visible` list and
  inside the `prefers-reduced-motion` block, so any "does this class appear in the
  stylesheet" check said yes while the button had no appearance at all. `declaredClasses`
  in `stylesheet_coverage_test.go` only counts an *unconditional* declaration, skips
  reduced-motion blocks and `@keyframes` bodies, and is why the deletion is caught
  rather than argued about.
- **`ops/web/templates` and `styles.css` drift silently** — Playwright asserts
  behaviour, and a raw native `<button>` passes every behavioural assertion.
  `TestTemplateClassesHaveStylesheetRules` is the gate. Classes with no rule must be
  added to `intentionallyUnstyled` with a reason; that register fails the build if an
  entry gains a rule or stops being used, so it cannot rot into a blanket exemption.
- **Every modal-bearing handler — create *and* update — must set
  `HX-Trigger: closeModal`** before returning its partial (DJI-440), or an
  edit leaves the modal open over an already-updated list. `hx-on:submit`
  cannot replace it: the native submit event has no `detail.successful`,
  and this app ships **htmx 1.9.10**, so htmx 2's `hx-on::after-request`
  does not exist here either.
- **Pongo2 `{# #}` comments cannot span lines** — keep template comments
  single-line.
- **Feature pages are pinned by their specs**: every page keeps a
  `.page-header h2`, and its region partial keeps the `.section-header` copy
  with the Add button, so the title legitimately appears twice. Deduping
  either one is a spec change, not a tidy — it needs all seven partials and
  five specs together, or it breaks 11 tests.
- **`c.Is("form")` is always false**: Fiber's MIME table has a `json` key
  but no `form` key, so the check never matches (it hid the
  unchecked-checkbox handling for watchlists, profiles and schedules). Test
  the header — `strings.HasPrefix(c.Get(fiber.HeaderContentType),
  fiber.MIMEApplicationForm)`, see `isFormPost` in `auth_context.go`.
- **Inline `<script>` never runs here**: `main.go` and the Caddyfile both
  set `script-src 'self'`, so script inside a template is dead code in every
  environment — handlers belong in `ops/web/static/js/app.js`.
- **Form posts need `form:` tags beside `json:`**, and an empty `<select>`
  cannot unmarshal into a `uuid.UUID` (the decoder fails the *whole* body,
  so the error reads `invalid request body`, not the field). Take such
  fields as `string` and parse; `uuid.Nil` also cannot be stored where a
  foreign key exists, so "use the global default" has to resolve the
  default row (`WatchlistHandler.resolveFormProfileID`, `artists.Add`).
- **A mutation response renders the whole region partial**, so its swap
  target must be the region with `innerHTML` (`#X-region`, as `/playlists`
  and `/jobs` already did). Targeting the region's inner `#X-list` with
  `outerHTML` nests a second region inside the first — a duplicate Add
  button and section title on every save.

- **`assert.Regexp(t, rx, str)` takes the string to match as the THIRD
  argument.** Passing the message there makes the guard compare your message
  against its own pattern and fail for a reason unrelated to the code under
  test. When an exact substring will do, use `assert.Contains` instead.
- **A template guard asserting "no attribute X" by substring false-positives on
  attribute VALUES.** `hx-include="closest [role='listitem']"` contains `role=`,
  so `assert.NotContains(openTag, "role=")` fails on correct markup. Match an
  attribute as `(?:^|\s)role="` — whitespace before it, and the quoting it takes.

## Consolidated workspace learnings (merged from DevWorks base, 2026-09-18)

> Repo-specific operational scar tissue. Where an entry overlaps a section
> above it adds nuance rather than replacing it.

### GitHub, CI & review workflow

- **`gh api` accepts neither `-o` nor `-w`** — use `--silent` / `--jq`.
  `gh pr checks --json name,state` reports **UPPERCASE** states
  (`PENDING`/`IN_PROGRESS`/`SUCCESS`), so poll for the uppercase set.

- Default branch is `master`, not `main`.
- **Never `git reset --hard origin/master` to sync after a merge — it silently
  destroys the user's uncommitted work.** Unstaged edits are never written to the
  object store, so there is no `git fsck` recovery. `git checkout master && git
  pull --ff-only` carries them across safely. An earlier note here said
  `reset --hard` was safe *because* master only fast-forwards; that is exactly
  the assumption that costs the most when it is wrong.
- Merge PRs with `gh pr merge N --repo pvnkmnk/djinn-netrunner --squash
  --delete-branch`. It often prints **nothing** on success (exit 0, empty
  stdout), and a "Merging…" line can precede a merge that never ran —
  confirm `gh pr view N --json state,mergeCommit` before assuming a merge
  landed or re-issuing it. To sync afterwards use
  `git checkout master && git pull --ff-only`, which carries uncommitted work
  across — NOT `git reset --hard origin/master`, see above.
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
- CodeRabbit is **manual here** (`@coderabbitai review`) and its OSS allowance
  is roughly one review per hour. A check reading `pass` can mean "rate
  limited" rather than reviewed — read the comment body, and don't hold a
  merge open indefinitely waiting for it.

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

### Linear: use `scripts/linear.py`, not the MCP connector

`scripts/linear.py` is how this repo reads and writes Linear. The MCP connector
still works for one-off lookups, but three of its behaviours make it the wrong
tool for anything load-bearing.

**`ProjectUpdateInput.content` is a separate field from `description`.**
`description` is the ~127-char one-line summary; the body is `content`, also
readable as `project.documentContent.content` (58KB for P-DJI-28). Editing a
project body is a read-modify-write of ONE field — `linear.py project-body`
prints a diff, writes, then re-reads. Never resend a whole description: it is a
blind write that no test can inspect.

**Linear normalises markdown on write.** A table separator sent as `| --- |`
stores as `| -- |`, and a trailing newline is stripped. Compare against what came
BACK, not what you sent — `project-body` warns when they differ, and that warning
is expected, not a failure.

**A body read back from the API is markdown, not elements.** A mention is
`[DJI-546](https://linear.app/djinnet/issue/DJI-546/...)`; `<issue>` /
`<pull-request>` are the connector's rendering. Flatten `[x](url)` to `x` before
asserting structure.

**Only a full `linear.app` URL becomes a link, and only in a body.** Measured
by writing the same four forms to a project body and to a comment and reading
back what Linear stored:

| written | project body | comment |
| --- | --- | --- |
| bare `DJI-591` | plain text | plain text |
| bare inside prose | plain text | plain text |
| `owner/repo#317` | plain text | plain text |
| `https://linear.app/.../DJI-593` | **becomes a link** | plain text |

So a resend MUST carry full URLs; bare identifiers and `owner/repo#NNN` render as
dead text. (An earlier entry here claimed the opposite — it was inferred from a
probe where a URL was present, and misattributed the linking.) The two surfaces
differ, so do not assume a form that links in one links in the other. A
`<pull-request>` element whose label disagrees with the PR is still normalised to
the PR title; the URL form sidesteps that.

**GraphQL field names and variable types are validated before execution.** Each of
these cost a 400 before it was pinned down:
  * `ProjectFilter` has NO `identifier` field (only `id`, `name`, `slugId`, ...).
    `project(id: "P-DJI-28")` takes a human identifier directly, so prefer it.
  * `project.id` is `ID`-typed: declaring `$pid: String` is a validation error.
  * `Query.issues` has no `project`/`state` argument — they belong in `filter:`.
  * **`IssueCreateInput` and `ProjectCreateInput` want OPPOSITE team fields.**
    The project needs `teamIds` (an ARRAY); the issue needs `teamId` (singular
    `String!`). Measured 2026-10-07 — a create using the project's array shape
    against an issue is refused naming `IssueCreateInput.teamId`, and the two
    complaints appear together, which reads like one input with two problems
    rather than "wrong shape for this mutation". Singular `teamId` on a
    *project* is rejected by name, so the error genuinely points both ways.
  * **`projectCreate.description` caps at 256 characters** (measured by
    bisection, not assumed). The long body goes in **`content`**, a separate
    field — the same `description` vs `documentContent.content` distinction
    below, and it bites on create as well as update. A body of any real length
    fails with `Argument Validation Error` and no field named, so bisect the
    length before suspecting the content.
  * `WorkflowState` has no `category`, but `type` is enough to move a ticket by
    name without hardcoding UUIDs.
  * `Issue.projectId` is NOT selectable — Linear answers `Cannot query field
    "projectId" on type "Issue". Did you mean "project"?`. Select
    `project { id }`. Print the HTTPError BODY when a raw request 400s: a bare
    "HTTP 400" names none of the four fields above and cost three blind retries.

**HTTP 200 can carry a populated `errors` array.** Every helper in the CLI
refuses to treat that as success — treating it as success is how a probe reads a
failed field as "empty" and reports data loss that never happened.

Rate limits are 2,500 requests/hour per user (shared across keys) and
3,000,000 complexity/hour, exposed as `X-RateLimit-*` headers, which the CLI reads
to throttle itself instead of discovering the ceiling by failing.

Credentials live in `~/.linear_token` (0600) or `LINEAR_API_KEY`; the drain token
in `~/.linear_drain_token` or `LINEAR_DRAIN_TOKEN`. **Never in the repo's `.env`**
— both app services declare `env_file: .env`, so a key there is injected into the
web and worker containers. The user's interactive shell exports never reach the
agent's shell either; a `0600` file outside the checkout is the only handover
that works.

**A filed description does not come back from `issue-list --json`** — the field is
omitted, so it reads as 0 chars and looks like data loss when nothing was lost.
Read a body back through the API (`{issue(id: "DJI-601"){description}}`) or MCP
`get_issue` before claiming a write landed. A create that returned a URL is not
proof the evidence survived Linear's markdown normalisation.

Tests: `python scripts/test_linear.py` (offline, no key). Mutation proof:
`python scripts/linear_mutation_check.py` (9/9 caught, 3/3 controls green).

### Linear webhooks (`ops/linear-webhook`)

Linear requires a **public, non-localhost HTTPS** endpoint, expects 200 within
**5 seconds**, retries only **3 times** (1 min / 1 hr / 6 hr), and may then
**disable the webhook until a human re-enables it**. That is why the receiver is
a Cloudflare Worker rather than a laptop listener: a machine that is asleep misses
events exactly when there is no retry budget left, and `cloudflared` here has no
`cert.pem`, so a quick tunnel would hand Linear a new random hostname on every
restart. Push to an always-on endpoint; the laptop drains the queue on demand.

The Worker verifies HMAC-SHA256 over the **raw** body against the
`linear-signature` header (constant-time compare), refuses a delivery whose
`webhookTimestamp` is more than 60s old, and dedupes on `Linear-Delivery` so
Linear's retries are acknowledged but not stored twice. `GET /events` exposes the
whole queue, so it is gated by `DRAIN_TOKEN` and fails closed when unset.
`updatedFrom` is retained — it is what makes a state change legible with no
follow-up query.

Tests: `cd ops/linear-webhook && node --test "test/*.test.mjs"` (14, offline).
Writing tests against it: Cloudflare's `KV.get(key, "json")` parses the stored
string, so a fake KV that ignores the type hint returns a raw string where the
Worker expects an object and fails as a phantom Worker bug.

Registration is scripted rather than a UI step — the signing secret is readable
via the API, so nothing sensitive is copy-pasted:

```bash
python scripts/linear.py webhook-register --url https://<worker>.workers.dev --print-secret
python scripts/linear.py events --url https://<worker>.workers.dev --since 0
```

### Linear MCP quirks

- **Every array argument arrives as an object** and fails schema validation:
  `save_issue.labels`, `list_issues.fields`. `priority` arrives as a string and
  fails; omit it. **Scalars work**, including `save_project.description` as one
  full string.
- **`patch` is unusable — do not spend calls rediscovering that.** It rejects a
  multi-op array, a bare op object, a *single-element array*, and a JSON string
  (all `expected array, received object`), on both `save_project` and
  `save_issue`. Linear's own API has no `patch` on `Mutation.projectUpdate`
  either (`Unknown argument "patch"`), so **a project description can only be
  changed by resending `description` in full** — not a connector limitation to
  work around, the only capability available.
- **Booleans are coerced to strings** and fail: `get_issue(includeRelations:
  "true")`. Omit the flag.
- Linear re-renders a GitHub PR URL as a `<pull-request>` element, and a bare
  markdown link inside prose sometimes nests duplicated ones. Reference a PR
  as plain `owner/repo#NNN`, never a URL, and always read a saved project
  description back to check for doubled elements.
- **A project body lives in `project.documentContent.content`, not
  `Project.description`** — the latter is the ~127-char one-line summary, so
  reading it to check a large body looks like catastrophic data loss and is not.
  The body is markdown and a mention is an ordinary link
  `[DJI-546](https://linear.app/…)`; the `<issue>`/`<pull-request>` elements are
  the connector's rendering. Flatten `[x](url)` → `x` before asserting structure.
- **Write full `linear.app` URLs, never bare identifiers or hand-authored
  `<issue id=…>` HTML.** Only the URL form links, and only in a body: bare
  `DJI-548` and plain `owner/repo#NNN` were both measured storing as plain text,
  in a body *and* in a comment. Hand-authored element HTML is also wrong, since
  the connector's rendering is not the storage form. Probe the round-trip on a
  throwaway before a full resend — one canceled issue buys certainty, and this
  entry was wrong once already.
- **The user's shell exports never reach the agent's shell** (each command is a
  fresh process from the orchestrator). Hand over a credential via a `0600` file
  outside the checkout; never `.env`, which both app services read via `env_file`
  and would inject into the web and worker containers.
- **Ticket state drifts from merges in both directions**, so reconcile it in the
  *same pass* as the merge (an operator standing rule): DJI-590 was marked Done
  before its merge landed, while DJI-524/529/534 stayed open long after their
  code shipped. Set state from the merged commit — never from the PR's intent —
  and post the commit hash as the evidence.

### Ad-hoc mutation harnesses

Beyond `scripts/mutation-check.sh`, one-off harnesses written to prove a
specific claim carry four traps, all of which inflate the score:

- **`[setup failed]` / `build failed` is VOID, not caught.** A mutation that
  does not compile fails every case including the control, and a harness that
  counts that as a catch reports 19/19 while proving nothing.
- **Every mutation must still compile.** Deleting `return f(tx, …)` inside a
  closure leaves `missing return` and voids the run — replace it with
  `return nil` so the mutation is semantic, not syntactic.
- **Include the package path in `go test`** (`./internal/api/ ./cmd/server/`).
  Omitting it makes every case fail via `[setup failed]`, i.e. all VOID.
- **Carry a benign CONTROL that must pass**, and re-anchor after any refactor —
  a stale anchor silently matches zero times and reads as a caught mutation.
- **Take the test name from `split()[2]`, not `split()[1]`.**
  `--- FAIL: TestName (0.00s)` splits to `['---', 'FAIL:', 'TestName', …]`, so
  `split()[1]` is the literal `FAIL:` — no mutation can then be classified
  caught and the run scores 0/3 with the intended test in fact failing.
- **A MISSED proves nothing until you confirm the mutant broke the invariant.**
  Substituting a 12-character password for a 12-rune floor still passes: that
  mutation measured nothing, it did not find a gap in the guard.
- **A guard over a script's *text* cannot see what the script produced at
  runtime.** Comparing a seeded bcrypt hash to the expected password is green
  while the seed skips an existing row and the database keeps the old value.

- **A hand-written package list is the weakest link in a harness.**
  `PKGS = ["./internal/services", "./internal/api"]` omits
  `backend/internal/api/templates`, which reads the same templates off disk — so
  the harness reported `10/10 mutations caught; both controls green` while that
  package failed four tests on the very file M8 mutates. A green tick means
  "those packages"; CI reads it as "the promise". Derive the list or declare
  the exclusion in the docstring.

A guard for "must *not* do X" needs an assertion of **absence**
(`assert.NotContains`), not just presence. Flipping `renderAdoptionOffer(c,
library, false)` to `true` passes any test that only asserts the offer is there.

- **A per-binding assertion can mask a mutation.** "the probe value rendered"
  passes when a second field carries the same string, so assert globally —
  e.g. *every* rendered `&` begins an entity.
- **A guard claiming "every template" must fail when a template has no row.**
  A table that renders one file and never counts the rest is the same blind
  spot as the five-guard suite DJI-545 replaced.
- **`go test` runs a vet subset, so a mutation that merely fails the COMPILER or
  vet is VOID, not caught.** Replacing a `fmt.Sprintf(msg, a, b)` with a
  constant string leaves unused args, vet rejects it, every case "fails", and
  the harness scores it as a catch. Mutate the whole `Sprintf(...)` expression,
  or the whole `if` block plus the import it needed, so the mutant still builds.
- **A boundary fixture must assert its own size before it is used as one.** A
  ten-word "passphrase" landed on 71 bytes and the test passed because the
  server accepted it — a green assertion measuring the wrong side of the line.
- **Prove the restore byte-identical BEFORE the control run.** A harness that
  mutates, rebuilds, and restores in a `finally` will happily run the control
  against a still-mutant tree, and the control passes for the wrong reason.

### Shell tooling: `rg` and `sd`, not `grep` and `sed`

- The user's standing preference: **`rg` over `grep`, `sd` over `sed`**. Both
  are installed (`rg` 15.2.0, `sd` 1.0.0).
- **`sd` rewrites files IN PLACE by default — there is no `-i` flag, and `-p` /
  `--preview` is the dry run.** It prints nothing and exits 0 whether it
  matched or not, so a bare `sd foo bar file.go` is an unverifiable write. That
  is the opposite of `sed`, where a bare `s///` only prints to stdout.
- `sd` preserves CRLF when it does rewrite a file, and with no file argument it
  reads stdin (`sed` stays usable for scripted, piped, non-interactive work —
  the CRLF patchers in `scripts/` rely on that).
- `rg` is the search tool here: `rg -n` for line numbers, `-g '*.go'` to glob,
  `-c` to count per file, `-l` filenames only. It honours `.gitignore`, so the
  large untracked `.agents/skills/` tree costs nothing.

### Build, test & integration

- **The `:memory:` SQLite test DB does not enforce foreign keys**, so a
  zero-UUID/FK violation passes the unit test and only fails on the live
  Postgres stack; assert the resolved value, not merely "not 400".
- **Templates, CSS and JS are baked into the images** (no bind mounts): a
  template or `app.js` change is invisible until
  `up -d --build`, and the running container's copy is what the browser gets.
- CI has no `gofmt` gate (the lint job is disabled pending
  golangci-lint+go1.25); `go vet ./...` is the gate, and `gofmt -w` would
  flip this repo's CRLF Go files to LF — format-check an LF copy instead.

- **`backend/internal/api/templates` is a separate package whose tests read
  `ops/web/templates/**` off disk.** `go test ./internal/api`, and any `-run`
  filter that names a package, will not run it — that gap shipped a red `test`
  job while local filtered runs were green. Name the package, or run `./...`.
- **`internal/services` can exceed go test's default 10m0s.** One ffprobe-backed
  download-probe test ran 9m46s on a loaded box while the package totals 177s
  unloaded and CI's whole `test` job 5m24s, so `panic: test timed out` there is
  a wall-clock hazard, not a failure: use `go test ./internal/services -timeout 30m`.
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
- **`ENVIRONMENT=production` and `CONFIG_ENV=production` are exported too**, and
  they fail the same package for a different reason, so clearing only `PORT`
  leaves three tests red and reads as a code bug. Measured 2026-10-07:
  `TestLoad_Defaults: cfg.Environment = "production", want "development"`,
  `TestLoad_ConfigEnvDefaultsToEnvironment`, and `TestLoad_JWTSecretAutoGenerated`
  (production without `JWT_SECRET` refuses to load). The whole set clears with
  `env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/config` -> `ok`.
  Check what the shell actually exports before believing a config failure:
  `env | grep -E '^(ENVIRONMENT|CONFIG_ENV|PORT|NETRUNNER)'`.
- **A probe must establish every precondition it asserts.** The ga-probes
  refusal probe asserted a library existed (correctly — its `toHaveLength(0)`
  over an empty list passes vacuously) but registered none, so it died on its
  own guard when `scripts/mutation-check.sh` ran it alone on a fresh DB. The full
  e2e suite passed on a library an earlier spec left behind: **an ordered suite
  hides a missing precondition, and only the isolated run finds it.** This was
  the third weekly `Mutation checks` failure in a row, each cycle's *control*
  run red. Run any probe standalone (`--grep <title>`, `env -u CI`) before
  believing it.
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
- Standing casing repro in the live deployment's library: `music/PUP/The Unraveling Of
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
  `.env.release.example` declared `NAVIDROME_ADMIN_PASSWORD` twice, so the
  placeholder below overrode the value a user set above it.
- Profile-gated services (Caddy behind `edge` in the base file, Navidrome
  behind `media-server` in the release overlay) disappear from
  `docker compose config` output entirely — correct, not a failed merge.
- `docker compose --env-file X` replaces `.env` for `${VAR}` *substitution*
  only; a service declaring `env_file: .env` still receives that file's
  values *inside the container*. `docker compose config` can print two
  different values for the same key and only one reaches the process —
  check which block a value came from before debugging.
- **Compose interpolation precedence is `shell env` > `--env-file` >
  `.env`**, and an exported variable silently wins over both. An agent shell
  that has sourced `.env` (or inherited those exports) therefore makes
  `docker compose --env-file ../.env.e2e ...` substitute `.env`'s values
  while every check still reads like the override worked. Measured on
  2026-10-08: `POSTGRES_PASSWORD`, `SLSKD_API_KEY`, `JWT_SECRET` and
  `SUBSONIC_PASSWORD` exported with `.env`'s values made the e2e stack ship
  `.env`'s password to both `postgres` and `ops-web` *against* a role
  initialised earlier from `.env.e2e`'s -- `FATAL: password authentication
  failed for user "musicops" (SQLSTATE 28P01)`, which I attributed to a
  password template defect for hours. `--env-file` is not broken: it loses.
  Before diagnosing compose, run `env | grep -E '^(POSTGRES_PASSWORD|
  SLSKD_API_KEY|JWT_SECRET|SUBSONIC_)'`; to bring the e2e stack up from such
  a shell, unset the nine keys `.env.e2e` defines (`env -u POSTGRES_PASSWORD
  -u SLSKD_API_KEY -u SLSKD_USERNAME -u SLSKD_PASSWORD -u ENVIRONMENT
  -u CONFIG_ENV -u JWT_SECRET -u SUBSONIC_ENABLED -u SUBSONIC_PASSWORD`) or
  align `.env.e2e` to `.env`'s values. A `psql -h 127.0.0.1` password probe
  is worthless here: `pg_hba.conf` carries `trust` for loopback, so *any*
  password "succeeds" -- probe over the container's compose-network IP.
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
- The base `docker-compose.yml` is the **dev** path and publishes the app on
  `${APP_BIND_ADDR:-127.0.0.1}:${APP_HTTP_PORT:-8080}:8080`;
  `docker-compose.release.yml` adds production semantics to the same stack.
  Host-side work (`scripts/smoke.sh`, curl `:8080`) needs one of them up, so
  only one stack can run at a time.
- **Version stamping has one rule — `scripts/version.sh`** (called by
  `deploy.sh` and the published-image workflow): `APP_VERSION` env override
  -> the exact git tag on HEAD -> the `dev` sentinel. Images tag as
  `djinn-netrunner-ops-{web,worker}:${APP_VERSION}`, and `deploy.sh` writes
  the resolved value back to `.env` so `smoke.sh` checks the version the
  image carries. A git tag with `/` or `+` is rejected (invalid Docker tag);
  override with `APP_VERSION`. `docker.yml` publishes to `ghcr.io` on `v*`
  tags under the same rule — a released image's footer shows its real
  version, any other build reports `NetRunner vdev` (deliberate sentinel,
  not a defect).
- **The beta path is retired (#288)** — dev and release are the only paths;
  rename map: CHANGELOG `[Unreleased]`; doc policy: `docs/MANIFEST.md`;
  `docs/BETA_ACCEPTANCE.md`/`docs/plans/*` are history, never instructions.
- The live deployment runs from a *separate clone*, not the working repo — sync
  changed files there and rebuild `ops-worker` before live verification, or you
  test the previous binary and report it as evidence.
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
  flag only affects the app's service-mesh client, so even on the release path a
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

- `.agents/skills/`: `release-readiness-review` (two-phase audit+closure) and
  `testing-watchlist-ui` (UI testing notes incl. resolved CSP/route issues).
  Repo-root `skills/` holds the task guides (`add-feature`, `fix-bug`,
  `run-tests`, `deploy`, `api-usage`, `data-model`, ...). The 2026-09-18
  `e2e-test-spec-generator`/`auto-linear-update` skills no longer exist.
- Read-only dependency clones (materialised on demand, not in a fresh clone)
  live under `.slim/clonedeps/repos/` for inspection (do not edit): `gofiber__fiber` v2.52.13 (middleware chain,
  context, routing), `go-gorm__gorm` v1.31.1 (query building, preloading,
  transactions, migrations), `mark3labs__mcp-go` v0.45.0 (MCP SDK, tool
  definitions, transports).

## Product facts that cost time to rediscover

- **No webfont is loaded anywhere.** `styles.css` names `Inter` and
  `JetBrains Mono`; the README names `Orbitron`. There is no `@font-face` and
  no font link in the repo, so every one resolves to a system fallback. Any
  screenshot showing the "brand" typeface is showing a fallback. See
  `PRODUCT.md`.
- **Before extending a service, read what its decode already drops.**
  `SearchArtist` captured only id/name/sort-name/disambiguation, so MusicBrainz's
  `country` and `type` — the two fields that actually separate same-named artists
  — were discarded before any UI could show them. `90605e4` (#311) added them.
  "The service cannot return that" is a claim to check in the decode struct.
- **An ambiguous artist must be chosen, not resolved.** `artists.Add` took
  `results[0]` behind a comment claiming a confidence check. Typing "Death"
  monitored **Napalm Death**. The picker in #311 is the fix; the bare-name
  `POST /api/artists` path still auto-accepts a top result deliberately, so the
  documented 201 keeps working — an API affordance, not the UI path.
- **A failed search and an empty search must not share a status.** `artists.Add`
  answered 404 for both "MusicBrainz found nothing" and "MusicBrainz was
  unreachable". #311 splits them (502 vs 404). See the htmx 4xx pitfall above for
  why the shared status was invisible rather than merely wrong.

## Baseline verification traps

Two checks that look like they pass but do not:

- **The Postgres role is `musicops`, not `netrunner`** — `psql -U netrunner`
  fails with `role does not exist` and reads like a broken container. The role
  and database are both `musicops` (`docker-compose.yml:33-34`), but the
  **password is `${POSTGRES_PASSWORD}` from `.env`**, not `musicops`. Read it
  from the running container rather than guessing:
  `docker exec netrunner-postgres printenv | sed -n 's/^POSTGRES_PASSWORD=//p'`.
- **The volume `immich_pgdata` is not a NetRunner leak.** It belongs to the
  Immich compose project. A `pgdata`-outside-compose check must exclude it, or
  it reports a phantom leak forever.
- **Dev-stack probe users are kept on purpose across sessions** — pruning an old
  owner's rows can manufacture the very defect under test (deleting the job of
  the user with no successor creates the owner-less job DJI-583 reports). Add
  fixtures; don't sweep old ones.

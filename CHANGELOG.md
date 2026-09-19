# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.0.3] - 2026-09-19

### Added
- The Soulseek entrance's wrong-work refusal, driven live (#259): the e2e
  overlay replaces slskd with a stand-in (`ops/fake-slskd`) speaking exactly
  the API surface the worker calls, serving a real FLAC tagged for a
  different work; plausibility and playability pass, the identity gate
  refuses, the library stays clean. The `ga-probes` suite also drives the
  multi-hop post-handover refusal live (DJI-500's documented residual): a
  hop the downloader performs itself is denied by the egress boundary, and
  the allowlist is documented as a default-deny feature
- Success-path proof for the boundary (C10): a matching Soulseek download
  passes the gate, imports, and is visible via the library API (#259)
- Automated mutation proofs (C9): `scripts/mutation-check.sh` applies a
  targeted mutation, expects the probe to FAIL, restores, and requires a
  passing control run; wired as a weekly scheduled CI workflow (#260)
  and proven on a real runner through three CI-environment fixes (#261,
  #262, #263). Covers the identity gate, the egress boundary, and — new
  this release — the success path (#267)
- Worker job-concurrency limit is env-configurable (`MAX_CONCURRENT_JOBS`,
  documented default, SQLite warning kept) (#256)

### Changed
- The e2e test-helper endpoints moved out of `cmd/server/main.go` into
  `internal/api/testapi`, conditionally registered behind
  `E2E_ENABLE_TEST_API`, with contract tests pinning the endpoint surface;
  the fake slskd's peer roster is configurable at seed time, so a new
  acceptance clause no longer needs a fixture-code change (#266)
- `ops/audio-probe` split into one role per file (wrong-work server, flip
  server, success source) behind a thin dispatcher (#267)

### Fixed
- Probe scope IDs based on `UnixNano` collide on Windows (~15ms clock
  granularity), reintroducing advisory-lock contention; a process-unique
  sequence suffix disambiguates them (#266, caught by the new contract test)
- Worker-driven e2e specs died on Playwright's 30s default timeout in CI
  while passing locally with an ad-hoc flag; per-test timeouts make the
  suite CI-safe on its own (#260)
- The wrong-work refusal probe's library assertion demanded a globally
  empty scan and could pass or fail on residue from another spec; it now
  scopes on the refused download's identity (#260)
- `ops/fake-slskd/entrypoint.py` had two statements fused on one line
  (CRLF-glue failure mode from #266); the crash looped any rebuild of the
  stand-in until fixed (#267)
- The mutation harness false-greened three ways before CI caught them:
  a Python 3 stub that exits 0, `Built` without recreate reusing the
  pre-mutation binary, and Actions' ambient `CI=true` flipping
  `reuseExistingServer` off mid-cycle (#260, #262, #263)

## [v0.0.2] - 2026-09-19

### Added
- Validating egress boundary in front of yt-dlp (#253, DJI-501): a Squid
  sidecar in the beta and e2e overlays denies private/loopback ranges at
  connect time and allowlists extraction domains; `YTDLP_PROXY` routes only
  the downloader through it. Proven live by `egress-refusal.spec.ts` and
  verified to bite via mutation (removing `YTDLP_PROXY` turns the spec red)
- Permissions and edge-case browser suite (#251, DJI-434): a real second user
  drives the authorization matrix, cross-owner playlist isolation is asserted
  in both directions, and a hostile-input sweep must never 500. Mutation
  proof recorded: removing the admin role check fails exactly the two
  admin-authorization tests

### Changed
- Terminal-outcome writers (`noResultsItem`/`failItem`/`abandonItem`) moved
  out of the import path into `item_outcomes.go`, and the staging sweep into
  `staging_cleanup.go` beside its owner; identity-test fixtures folded into
  shared helpers (#250)
- The e2e fixture resolves the docker binary explicitly instead of relying on
  PATH, so admin promotion works on Windows shells (#252)
- The e2e worker points at the test database, and the egress proxy is a
  health-gated dependency of the worker in both overlays (#253)

### Fixed
- A stale scope-less acquisition job could requeue-loop and starve every
  later acquisition: seeded/production jobs now carry a unique advisory-lock
  scope (#253, found during the refusal probe)

## [v0.0.2-beta.1] - 2026-09-18

### Added
- Beta deployment assets: `docker-compose.beta.yml`, `.env.beta.example` and
  `docs/BETA_DEPLOYMENT.md`, so a beta can be brought up from a clone instead of
  reverse-engineered from a running stack (#229, #236)
- `docs/BETA_ACCEPTANCE.md`: the acceptance matrix and its evidence, with each row
  naming the command and the output it observed (#229, #232, #241)
- Subsonic surface a client can actually browse: stable artist ids, `getArtist`, and
  no empty-named placeholder artist in `search3` (#209, #210, #215, #228)
- Repair tooling for the libraries earlier versions built: fragmented-album merge with
  a runbook (#222, #224) and canonical identity tag repair
  (`netrunner-cli library repair-tags`, dry run -> backup -> apply) (#241)
- Staged-download ownership plus a janitor that reclaims what no live item owns
  (#235, #236)
- Browser suite in CI and locally: `scripts/e2e.sh`, `.env.e2e.example`, and the
  artists/playlists/jobs/admin specs (#244)
- `netrunner-cli` in the runtime image and a `netrunner-backups` volume, because the
  documented repair commands and their backups did not exist in a deployment (#241)

### Changed
- The worker talks to a Subsonic-compatible media server: Navidrome joins the
  integration stack and gonic becomes optional (#209, #215)
- Acquisition outcomes are honest: `failed`, `partial` and `abandoned` replace silent
  success, and a gate rejection is terminal instead of retried (#211, #242)
- Artist and album identity is canonical everywhere - folders, tags and dedup fold
  case and credits the same way (#219, #230, #231, #241)
- Configuration fails fast: production refuses to boot without `JWT_SECRET`,
  `SUBSONIC_PASSWORD` or the media-server URL rather than degrading (#229)

### Fixed
- `SLSKD_API_KEY` never reached slskd, so every search returned 401 (#226)
- A peer queued remotely stalled the whole job queue for the full wait budget (#228)
- Junk and implausible downloads reached the library; a minimum-size and `ffprobe`
  gate plus a download-identity check now refuse anything that is not the requested
  recording, on both entrances into the import stage (#228, #239, #240)
- `audiometa` panicked on MP4 `covr`; tag writes moved to `ffmpeg -c copy`, which also
  fixed M4A/OGG writes (#223)
- Staging accumulated leftovers: the sweep only removed empty directories, a
  permissions mismatch left slskd unable to write, and the discard path assumed a
  staging root it never enforced (#220, #231, #233, #234)
- A duplicate library path returned a bare 500 instead of 409 (#229)
- Sessions were invalidated on every restart because `JWT_SECRET` never reached the
  process (#229)
- The jobs filters were dead controls: `hx-trigger` on the wrapper div never fired in
  htmx 1.9, and the filtered swap nested a second copy of the filters inside the list
  (#244)
- The e2e suite shared the beta deployment's compose project, so its `down -v` deleted
  the beta library (#244)

## [v0.0.1] - 2026-06-16

### Infrastructure
- Added Node.js 22 LTS and fresh yt-dlp (pip) to Docker runtime image, configurable via `YTDLP_PATH` env var
- Added multi-environment YAML config support via `CONFIG_ENV` (`config.yaml`, `config.<env>.yaml`)
- Added LiteFS configuration for SQLite multi-worker scaling
- Fixed CI workflows to properly skip integration-tagged tests in unit test runs
- Documented `YTDLP_PATH` and `CONFIG_ENV` in `.env.example`

### Testing
- Added auth E2E smoke test (register, login, logout, rate limiting)
- Added watchlist, library, and job E2E smoke tests
- Added webhook delivery and quota warning E2E tests
- Performed mobile navigation and keyboard accessibility audit with fixes

### Admin Panel
- Added admin panel backend: user CRUD, role management, password reset, audit logging, system config editor
- Added admin panel frontend: user management, audit log viewer, system config editor pages (HTMX)
- Added admin route authorization tests
- Added admin panel integration smoke test

### Release
- Initial public release v0.0.1

## [0.0.1] — 2026-05-XX

Initial release of Djinn NetRunner.

### Added

- **Core Platform**: Go 1.25 backend with Fiber HTTP framework, HTMX/Pongo2 server-rendered UI, background worker orchestrator, MCP agent interface (20 tools), CLI management tool
- **Music Acquisition**: Automated discovery from Spotify/Last.fm/ListenBrainz/RSS/local files, Soulseek acquisition via slskd, quality profiles, peer reputation scoring
- **Metadata & Enrichment**: MusicBrainz, AcoustID, Discogs, Last.fm, ListenBrainz providers; audio fingerprinting; metadata tagging and cover art; persistent cache
- **Library Management**: Multi-library support with scanning/indexing, track pruning, Gonic/Navidrome integration, disk quota tracking
- **Monitoring**: Artist release tracking via MusicBrainz, webhook notifications, real-time WebSocket events and job console
- **Security**: Session-cookie auth with RBAC, BOLA enforcement, WebSocket ownership, HTTPOnly/Secure/SameSite cookies, proxy-aware HTTP client factory, auth rate limiting
- **Infrastructure**: Docker Compose stack (PostgreSQL 16 + slskd + Gonic + Caddy), SQLite WAL for local dev, LiteFS guard, PostgreSQL advisory locks, GORM auto-migrations
- **Reliability**: Worker panic recovery, graceful shutdown with drain timeout, zombie job cleanup, context nil guard
- **Documentation**: AGENTS.md (repo map, API reference, MCP tool schemas, DB driver behavior), ops runbooks (backup, upgrade, DR, SQLite→Postgres migration), database tier guidance
- **CI/CD**: Unit tests + go vet + govulncheck in CI, integration test workflow with dockerized Postgres/slskd, Docker build to ghcr.io, PR review automation

### Detailed Cycle History

#### Cycle 7 — Security & Stability Hardening

- **DJI-370**: Centralized proxy-aware HTTP client factory (`NewProxyAwareHTTPClient`); wired through all 12 service constructors (PR #144)
- **DJI-361/356/357**: Worker panic recovery, shutdown hang fix, context nil guard (PR #143)
- **DJI-363**: SQLite vs PostgreSQL support tier documentation in README and AGENTS.md; startup warning for SQLite + concurrent workers
- **DJI-374**: MCP tool schema reference table (20 tools with input/output/idempotency) in AGENTS.md
- **DJI-373**: Ops runbooks: backup, upgrade, disaster recovery, SQLite-to-Postgres migration (PR #145)
- **DJI-372**: Integration test CI workflow (`.github/workflows/integration.yml`)
- **DJI-375**: v0.0.1 release checklist and CHANGELOG

#### Cycle C — Release Preparation

- **DJI-320**: Remove legacy `conductor/` directory (archived planning docs)
- **DJI-317**: Add ProxyURL validation at startup with `net/url.Parse` check
- **DJI-317**: Log warning in slskd_service.go on invalid proxy URL (was silently ignored)
- **DJI-318**: Add contextual action hints to all empty states in partial templates
- **DJI-314**: Clean up README — remove stale beta/2.2 references, fix badge to v0.0.1, fix tree indentation
- **DJI-315**: Add bash smoke test (`scripts/smoke-test.sh`) and Go integration smoke tests

#### Cycle 6 Security (PR #136)

- **DJI-321**: Replace 39 `err.Error()` leaks across 6 API handler files with server-logged generic responses; add `internalServerError` helper; fix `validateLibraryPath` filesystem path leak
- **DJI-322**: Make empty `GONIC_USER`/`GONIC_PASS` a fatal startup error in production mode
- **DJI-323**: Add `--` separator before URL in yt-dlp `exec.Command` to prevent option injection
- **DJI-324**: Add `--` separator before file path in fpcalc `exec.Command`
- **DJI-325**: SameSite=Lax already set on auth cookies (verified, no change needed)

#### Cycle B (PR #133, #134)

- **DJI-308**: Profile service CGO fix — switch from `gorm.io/driver/sqlite` to `glebarez/sqlite` (pure Go); extract `MockProvider` to shared `testutil` package
- **DJI-309**: Integration pipeline tests — 5 AC scenarios (sync→acquisition, full pipeline, download failure, metadata enrichment fallback, concurrent jobs)
- **DJI-310**: Fix stale `NewSlskdService` constructor in integration harness
- **DJI-311**: Library Browse UI — searchable/sortable/paginated track table with HTMX partial updates and detail modal
- **DJI-312**: PruneTracks job logging — jobID parameter, per-file JobLog entries (OK/ERR/INFO summary), `error_detail` on failure
- **DJI-313**: Bandcamp RSS support with channel-title-as-artist fallback
- Job UI improvements: cancel (hx-delete → hx-post /cancel), Retry button for failed jobs, Attempt/ErrorDetail display
- Ownership scoping: non-admin users only see their own jobs; ownership validation on retry/cancel

#### Cycle A — Foundation & CI/CD (PR #131)

- **DJI-303**: Split server/worker in Docker Compose; simplified `entrypoint.sh` to single-process bootstrap
- **DJI-305**: Audit Go dependencies; `govulncheck` clean after bumping `golang.org/x/net`
- **DJI-302**: Docker CI — build and push to GHCR on main push and v\* tags (Buildx + GHA cache)
- **DJI-304**: Enhanced health endpoint (`/api/health`) with per-dependency checks (db, slskd, gonic, disk); returns ok/degraded status
- **DJI-306**: Blocking `govulncheck` in CI (no longer `continue-on-error`)
- **DJI-307**: Compose healthchecks — wget for ops-web, `kill -0 1` for ops-worker; Caddy depends-on `service_healthy`

#### Pre-Cycle Foundation

- Comprehensive test isolation: SQLite `:memory:` for auth/authorization tests, UUID-based test data, defer cleanup
- Cross-platform fixes: `os.TempDir()` for library path tests, nil-checks for ListenNotify
- Security hardening: bcrypt cost 10→12, XSS escape in job templates, cookie Secure/SameSite configuration
- Dependency bump: `gofiber/fiber/v2` to v2.52.13 (CVE-2026-42554)
- Docs reconciliation: `.env.example`, AGENTS.md, ARCHITECTURE.md alignment with runtime behavior

[Unreleased]: https://github.com/pvnkmnk/djinn-netrunner/compare/v0.0.3...HEAD
[v0.0.3]: https://github.com/pvnkmnk/djinn-netrunner/compare/v0.0.2...v0.0.3
[v0.0.2]: https://github.com/pvnkmnk/djinn-netrunner/compare/v0.0.2-beta.1...v0.0.2
[v0.0.2-beta.1]: https://github.com/pvnkmnk/djinn-netrunner/compare/v0.0.2-b...v0.0.2-beta.1
[0.0.1]: https://github.com/pvnkmnk/djinn-netrunner/releases/tag/v0.0.1

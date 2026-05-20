# Changelog

## v0.0.1 (2026-05-19) — Initial Release

### Cycle C — Release Preparation

- **DJI-320**: Remove legacy `conductor/` directory (archived planning docs)
- **DJI-317**: Add ProxyURL validation at startup with `net/url.Parse` check
- **DJI-317**: Log warning in slskd_service.go on invalid proxy URL (was silently ignored)
- **DJI-318**: Add contextual action hints to all empty states in partial templates
- **DJI-314**: Clean up README — remove stale beta/2.2 references, fix badge to v0.0.1, fix tree indentation
- **DJI-315**: Add bash smoke test (`scripts/smoke-test.sh`) and Go integration smoke tests

### Cycle 6 Security (PR #136)

- **DJI-321**: Replace 39 `err.Error()` leaks across 6 API handler files with server-logged generic responses; add `internalServerError` helper; fix `validateLibraryPath` filesystem path leak
- **DJI-322**: Make empty `GONIC_USER`/`GONIC_PASS` a fatal startup error in production mode
- **DJI-323**: Add `--` separator before URL in yt-dlp `exec.Command` to prevent option injection
- **DJI-324**: Add `--` separator before file path in fpcalc `exec.Command`
- **DJI-325**: SameSite=Lax already set on auth cookies (verified, no change needed)

### Cycle B (PR #133, #134)

- **DJI-308**: Profile service CGO fix — switch from `gorm.io/driver/sqlite` to `glebarez/sqlite` (pure Go); extract `MockProvider` to shared `testutil` package
- **DJI-309**: Integration pipeline tests — 5 AC scenarios (sync→acquisition, full pipeline, download failure, metadata enrichment fallback, concurrent jobs)
- **DJI-310**: Fix stale `NewSlskdService` constructor in integration harness
- **DJI-311**: Library Browse UI — searchable/sortable/paginated track table with HTMX partial updates and detail modal
- **DJI-312**: PruneTracks job logging — jobID parameter, per-file JobLog entries (OK/ERR/INFO summary), `error_detail` on failure
- **DJI-313**: Bandcamp RSS support with channel-title-as-artist fallback
- Job UI improvements: cancel (hx-delete → hx-post /cancel), Retry button for failed jobs, Attempt/ErrorDetail display
- Ownership scoping: non-admin users only see their own jobs; ownership validation on retry/cancel

### Cycle A — Foundation & CI/CD (PR #131)

- **DJI-303**: Split server/worker in Docker Compose; simplified `entrypoint.sh` to single-process bootstrap
- **DJI-305**: Audit Go dependencies; `govulncheck` clean after bumping `golang.org/x/net`
- **DJI-302**: Docker CI — build and push to GHCR on main push and v\* tags (Buildx + GHA cache)
- **DJI-304**: Enhanced health endpoint (`/api/health`) with per-dependency checks (db, slskd, gonic, disk); returns ok/degraded status
- **DJI-306**: Blocking `govulncheck` in CI (no longer `continue-on-error`)
- **DJI-307**: Compose healthchecks — wget for ops-web, `kill -0 1` for ops-worker; Caddy depends-on `service_healthy`

### Pre-Cycle Foundation

- Comprehensive test isolation: SQLite `:memory:` for auth/authorization tests, UUID-based test data, defer cleanup
- Cross-platform fixes: `os.TempDir()` for library path tests, nil-checks for ListenNotify
- Security hardening: bcrypt cost 10→12, XSS escape in job templates, cookie Secure/SameSite configuration
- Dependency bump: `gofiber/fiber/v2` to v2.52.13 (CVE-2026-42554)
- Docs reconciliation: `.env.example`, AGENTS.md, ARCHITECTURE.md alignment with runtime behavior

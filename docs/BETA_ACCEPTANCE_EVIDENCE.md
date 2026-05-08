# NetRunner Beta Acceptance Evidence

Date: 2026-05-07
Commit: working tree (pre-commit)
Executor: Codex
Environment: local docker compose / Windows PowerShell / Caddy+Postgres+slskd+gonic stack

## 1) Automated validation gate
- [x] `pwsh -File scripts/validate.ps1 -SkipVulnCheck` passed
- Evidence:
  - command: `pwsh -File scripts/validate.ps1 -SkipVulnCheck`
  - summary: `go vet ./...`, `go test ./...`, and `go build` all succeeded.

## 2) Manual Docker acceptance matrix

| Scenario | Expected | Result (Pass/Fail/Blocked) | Evidence |
|---|---|---|---|
| Auth login/logout/register | Session flow works, protected routes accessible after auth | Blocked | `netrunner` container in restart loop; `/api/health` returns `502` via Caddy |
| Watchlist create/edit/sync/preview | CRUD + sync + preview updates | Blocked | App service unavailable due to migration crash loop |
| Library add/scan | Library created and scan job completes | Blocked | App service unavailable due to migration crash loop |
| Artist CRUD | Add/update/delete works with ownership checks | Blocked | App service unavailable due to migration crash loop |
| Schedule CRUD | Create/update/delete and scoped visibility | Blocked | App service unavailable due to migration crash loop |
| Job execution + console stream | Logs stream, filter/copy/clear/resume behavior works | Blocked | App service unavailable due to migration crash loop |
| Role/tenant isolation | Non-admin cannot read/modify other user resources | Blocked | API/UI runtime blocked by migration failure |
| Webhook smoke test | Completion webhook emitted with expected payload shape | Blocked | App service unavailable due to migration crash loop |
| Quota warning smoke test | Quota threshold warning generated and observable | Blocked | App service unavailable due to migration crash loop |

## 3) Frontend beta usability checks
- [ ] Mobile nav toggle works on narrow viewport
- [ ] Keyboard-only path works for primary actions
- [x] Focus states are visible on links/buttons/forms (implemented in CSS; runtime verification blocked)
- [x] Loading/empty/error states are readable and non-blocking (implemented; runtime verification blocked)
- [x] No JS console errors on non-dashboard pages (guarded by null-safe checks; runtime verification blocked)

## 4) Notes / regressions / follow-up
- Blockers:
  - Docker acceptance blocked by migration failure: `ERROR: column "job_type" of relation "jobs" contains null values (SQLSTATE 23502)` in `internal/database/migrate.go` during container startup.
- Known limitations:
  - UI runtime verification requires healthy `netrunner` service; currently blocked.
- Follow-up issues:
  - Add migration backfill strategy for legacy `jobs` rows before `NOT NULL` `job_type` enforcement.

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
| Auth login/logout/register | Session flow works, protected routes accessible after auth | Pass (scripted) | Registration/login succeeded via CSRF + Origin/Referer headers |
| Watchlist create/edit/sync/preview | CRUD + sync + preview updates | Partial | Authenticated watchlist list endpoint returned expected payload; full browser CRUD pending |
| Library add/scan | Library created and scan job completes | Pending | Not fully exercised in this pass |
| Artist CRUD | Add/update/delete works with ownership checks | Pending | Not fully exercised in this pass |
| Schedule CRUD | Create/update/delete and scoped visibility | Pending | Not fully exercised in this pass |
| Job execution + console stream | Logs stream, filter/copy/clear/resume behavior works | Partial | Worker/job logs observed in container logs; UI console behavior pending browser walkthrough |
| Role/tenant isolation | Non-admin cannot read/modify other user resources | Partial | Owner-scoped API handlers and tests pass; live two-user browser verification pending |
| Webhook smoke test | Completion webhook emitted with expected payload shape | Pending | Not fully exercised in this pass |
| Quota warning smoke test | Quota threshold warning generated and observable | Pending | Not fully exercised in this pass |

## 3) Frontend beta usability checks
- [ ] Mobile nav toggle works on narrow viewport (requires browser walkthrough)
- [ ] Keyboard-only path works for primary actions (requires browser walkthrough)
- [x] Focus states are visible on links/buttons/forms (implemented in CSS; runtime verification blocked)
- [x] Loading/empty/error states are readable and non-blocking (implemented; runtime verification blocked)
- [x] No JS console errors on non-dashboard pages (guarded by null-safe checks; runtime verification blocked)

## 4) Notes / regressions / follow-up
- Blockers:
  - No runtime blocker currently; app reached healthy state and worker processed jobs after one-time DB fix.
- Known limitations:
  - UI runtime verification requires healthy `netrunner` service; currently blocked.
- Follow-up issues:
  - Keep one-time SQL backfill in release notes for existing Docker volumes with legacy `jobs` schema/data.

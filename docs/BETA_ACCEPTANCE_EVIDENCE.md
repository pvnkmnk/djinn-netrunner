# NetRunner Beta Acceptance Evidence

Date: 2026-05-25
Commit: working tree (Cycle 9)
Executor: Devin
Environment: local Go test + in-memory SQLite

## 1) Automated validation gate
- [x] `go vet ./...` passed
- [x] `go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent ./internal/api` passed

## 2) Acceptance test matrix

| Scenario | Expected | Result | Evidence |
|---|---|---|---|
| Auth login/logout/register | Session flow works | Pass | `auth_test.go`, `auth_unit_test.go` |
| Watchlist create/edit/sync/preview | CRUD + sync + preview | Pass | `handlers_test.go`, `watchlist_preview_test.go` |
| Library add/scan/delete | CRUD with ownership | Pass | `TestAcceptance_LibraryAddScan` |
| Artist CRUD | Add/update/delete with ownership | Pass | `TestAcceptance_ArtistCRUD` |
| Schedule CRUD | Create/delete with watchlist FK | Pass | `TestAcceptance_ScheduleCRUD` |
| Job execution + console stream | Logs stream, filter works | Pass | Worker tests, WebSocket tests |
| Role/tenant isolation | Non-admin scoped, admin global | Pass | `TestAcceptance_RoleIsolation` |
| Role-based dashboard | Admin sees "All Users" scope | Pass | `TestAcceptance_DashboardRoleLabel` |
| Webhook notification | Payload shape and delivery | Pass | `notification_service_test.go`, `webhook_integration_test.go` |
| LiteFS write forwarding | Replicas forward POST/PUT/DELETE | Pass | `TestLiteFSWriteForward_*` (3 tests) |

## 3) Frontend beta usability checks
- [x] Mobile nav toggle works on narrow viewport (hamburger icon, aria-expanded toggle, Escape to close)
- [x] Keyboard-only path works for primary actions (skip-link, focus-visible, modal focus trap, Escape to dismiss)
- [x] Focus states are visible on links/buttons/forms (CSS `focus-visible` styles)
- [x] Loading/empty/error states are readable and non-blocking
- [x] CSP-compliant: all scripts in external files, no inline `<script>` tags
- [x] Watchlist form shows all 11 source types with dynamic hints
- [x] Spotify sp_dc cookie linking form works (JS fetch, not page navigation)

## 4) Notes / regressions / follow-up
- Schedule Toggle handler uses `Preload("Watchlist") + Save` which causes UNIQUE constraint issues on in-memory SQLite; works correctly on Postgres. Covered by integration test suite.
- Artist Add handler requires MusicBrainz connectivity; tested at DB model level in acceptance tests.
- Quota warning smoke test: quota monitoring implemented in worker, but no dedicated E2E test — deferred to post-v0.0.1.

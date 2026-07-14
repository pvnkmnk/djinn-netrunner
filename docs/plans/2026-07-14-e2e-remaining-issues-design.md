# E2E Remaining Test Issues — Design

> **Date:** 2026-07-14
> **Status:** Approved, transitioning to implementation
> **Context:** After PR #187 fixed 6 E2E test suites (Libraries, Auth, Dashboard, Profiles, Jobs, Admin), 6 suites remain with pre-existing failures.

## Strategy

**Two-phase grouped PRs** to keep scope manageable:

- **Phase 1 (PR #188):** Infrastructure + Simple fixes — Subsonic, Permissions, Playlists, Artists, Schedules
- **Phase 2 (PR #189):** Complex — Watchlists (31 failures)

---

## Phase 1: Infrastructure + Simple Fixes

### DJI-433 — Subsonic (~15/17 failures)

**Root Cause:** Subsonic API routes exist in `main.go` but are gated behind `SUBSONIC_ENABLED` env var (default `false`). The test hits all GET endpoints and gets 404 because the router group isn't mounted. POST endpoints (createPlaylist, startScan) return 403 because CSRF middleware blocks them.

**Fix:**
1. Add `SUBSONIC_ENABLED=true` to `.env.e2e` — mounts all `/rest/*` Subsonic routes
2. Subsonic uses REST auth via query params (`u`, `p`, `v`, `c`, `f`), not session cookies — CSRF middleware won't apply since those routes are in a separate router group that handles auth internally

**Verification:** ~15 tests go from 4xx→200 in one shot. Remaining failures would be genuine bugs.

### DJI-434 — Permissions (2/22 failures)

**Root Cause:** Two tests in `permissions.spec.ts` have assertion values that don't match the rendered UI:
- Admin nav link visibility for regular user — CSS selector or text match mismatch
- Admin role badge — expected badge text doesn't match rendered HTML

**Fix:** Read test assertions and compare against actual template output. Adjust test expectations or template markup.

### DJI-430 — Playlists (2/20 failures)

**Root Cause:** Two timing/capability issues:
- "Playlists region loads" — HTMX region doesn't populate in time (timeout)
- "Delete playlist with confirm" — confirm dialog interaction times out

**Fix:** Add HTMX wait helpers, increase timeouts, or fix confirm dialog selector.

### DJI-428 — Artists (4/26 failures)

**Root Cause:** Four test mismatches:
- Card structure — expected HTML layout differs from rendered card
- Missing MBID — what renders when MBID is null/empty
- Pause monitoring HTMX — action times out
- Resume monitoring HTMX — action times out

**Fix:** Assertion adjustments + HTMX timing configuration.

### DJI-429 — Schedules (TBD)

**Root Cause:** Already fixed `quality_profile_id` in PR #187. May have remaining small issues.

**Fix:** Run tests, address any remaining <3 test failures.

---

## Phase 2: Watchlists — DJI-426 (~31/48 failures)

### Root Cause Categories

**A. Provider Validation Failures (bulk: ~20 failures)**
Every provider's `ValidateConfig()` is called during watchlist creation. Many fail in test environment:
- `local_file` / `local_directory` — `os.Stat()` fails because paths don't exist in container
- Other providers — URI format validation, missing API keys, or unreachable URLs

**Fix approach:** Read each provider's `ValidateConfig()`, then for each:
- Create required filesystem paths (like library create-dir pattern)
- Adjust test `source_uri` values to pass URI format checks
- For providers needing real credentials: mark those tests as conditional/skip in CI

**B. Card Selector Mismatch (~10 failures)**
Tests use `#watchlist-${numericId}` but template likely renders UUID-based IDs. Cascading to toggle, sync button, edit, and detail tests.

**Fix approach:** Check template's ID format, update test selectors to match, or add HTML `data-` attributes.

**C. HTMX Interaction Timing (~3 failures)**
- Modal overlay close doesn't work
- Sync button click times out
- Page context closes

**Fix approach:** HTMX wait helpers, proper assertions over raw waits.

### Fix Order
1. Diagnose A first (unblocks most tests)
2. Fix B (cascading to ~10 tests)
3. Fix C (independent)

---

## Infrastructure Changes

- **`.env.e2e`:** Add `SUBSONIC_ENABLED=true` (Subsonic routes)
- **`setup-test-db.sh`:** Already handles quality profile seed and DB setup (PR #187)

---

## Verification Plan

For each phase:
1. Build Docker images: `docker compose --env-file .env.e2e -f docker-compose.yml -f docker-compose.e2e.yml build`
2. Start stack with `setup-test-db.sh` (wired as Playwright webserver)
3. Run targeted test file: `npx playwright test tests/<spec>.spec.ts`
4. Verify all tests in the spec pass
5. For Phase 2: also run full suite to check no regressions on Phase 1 fixes

---

## Files to Modify

### Phase 1
- `.env.e2e` — Add `SUBSONIC_ENABLED=true`
- `e2e/tests/permissions.spec.ts` — Fix 2 assertions
- `e2e/tests/playlists.spec.ts` — Fix 2 timing/locator issues
- `e2e/tests/artists.spec.ts` — Fix 4 assertions/HTMX
- `e2e/tests/schedules.spec.ts` — Fix any remaining failures
- `e2e/tests/subsonic.spec.ts` — Minor assertion adjustments

### Phase 2
- `e2e/tests/watchlists.spec.ts` — Fix provider validation, selectors, HTMX timing

---

## Success Criteria

- **Phase 1:** All 5 remaining suites pass at >= 95% (accept pre-existing infrastructure limits)
- **Phase 2:** Watchlists pass >= 90% (accept SKIP for provider types requiring external auth)
- **Total E2E coverage:** 300+ tests across 12 spec files running reliably

> Release gate: Phase 1 passes >= 95% with only documented skips; Phase 2 passes >= 90%; no regressions on previously passing suites.

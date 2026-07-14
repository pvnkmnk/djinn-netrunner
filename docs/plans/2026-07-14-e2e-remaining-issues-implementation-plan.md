# E2E Remaining Issues — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all remaining E2E test failures across 6 test suites in two phases.

**Architecture:** Two-phase grouped PRs. Phase 1: Subsonic env var + simple assertion/timing fixes across 5 spec files. Phase 2: Diagnose and fix watchlist provider validation, card selectors, and HTMX timing.

**Tech Stack:** Playwright (TypeScript), Docker Compose, Go backend with Fiber

---

## Phase 1: Infrastructure + Simple Fixes

### Task 1: Enable Subsonic API in E2E env

**Files:**
- Modify: `.env.e2e:1-6`

**Step 1: Add SUBSONIC_ENABLED=true to .env.e2e**

```
# E2E Test Environment Variables
POSTGRES_PASSWORD=testpass
...
SUBSONIC_ENABLED=true
```

**Step 2: Run Subsonic tests**

Run: `cd e2e && npx playwright test tests/subsonic.spec.ts`
Expected: ~15 previously failing tests now pass (were 404, now 200)

**Step 3: Commit**

```bash
git add .env.e2e
git commit -m "fix(e2e): enable Subsonic API for E2E tests"
```

---

### Task 2: Fix Permissions test assertions

**Files:**
- Read: `e2e/tests/permissions.spec.ts` (lines around 180 and 373)
- Read: Relevant HTML templates to find actual rendered text/selectors

**Step 1: Read permissions spec failure error context**

Read: `e2e/tests/permissions.spec.ts` around the two failing tests
Read: `ops/web/templates/` relevant partials to find actual rendered output

**Step 2: Fix assertions to match actual rendered UI**

Adjust the two failing assertions (nav link visibility, role badge).

**Step 3: Commit**

```bash
git add e2e/tests/permissions.spec.ts
git commit -m "fix(e2e): fix permissions test assertions to match rendered UI"
```

---

### Task 3: Fix Playlists test timing

**Files:**
- Modify: `e2e/tests/playlists.spec.ts`

**Step 1: Read playlists spec failure error context**

Check the two failing tests (region load, delete confirm).

**Step 2: Fix timing/locator issues**

Add HTMX wait helpers or increase timeouts.

**Step 3: Commit**

```bash
git add e2e/tests/playlists.spec.ts
git commit -m "fix(e2e): fix playlists test timing and locator issues"
```

---

### Task 4: Fix Artists test assertions

**Files:**
- Modify: `e2e/tests/artists.spec.ts`

**Step 1: Read artists spec failures**

Read error context for 4 failures (card structure, MBID, pause, resume).

**Step 2: Fix each assertion**

Compare test expectations against actual rendered template output. Adjust assertions. Check if pause/resume HTMX actions need different triggers.

**Step 3: Commit**

```bash
git add e2e/tests/artists.spec.ts
git commit -m "fix(e2e): fix artists test card structure, MBID rendering, pause/resume"
```

---

### Task 5: Verify and fix Schedules tests

**Files:**
- Modify: `e2e/tests/schedules.spec.ts` (if needed)

**Step 1: Run schedules tests**

Run: `cd e2e && npx playwright test tests/schedules.spec.ts`
Check if any failures beyond the already-fixed `quality_profile_id`.

**Step 2: Fix any remaining failures**

**Step 3: Commit**

```bash
git add e2e/tests/schedules.spec.ts
git commit -m "fix(e2e): fix remaining schedules test issues"
```

---

### Task 6: Phase 1 full verification

**Step 1: Run all Phase 1 spec files**

```bash
cd e2e && npx playwright test tests/subsonic.spec.ts tests/permissions.spec.ts tests/playlists.spec.ts tests/artists.spec.ts tests/schedules.spec.ts
```

Expected: All tests pass (or only known pre-existing infrastructure limits).

**Step 2: Push and create PR #188**

```bash
git push origin HEAD
gh pr create \
  --title "Phase 1: Subsonic, Permissions, Playlists, Artists, Schedules E2E fixes" \
  --body "See docs/plans/2026-07-14-e2e-remaining-issues-design.md" \
  --base main
```

---

## Phase 2: Watchlists (DJI-426)

### Task 7: Diagnose provider validation failures

**Files:**
- Read: `backend/internal/services/watchlist/providers/*.go` — ValidateConfig() per provider
- Read: `e2e/tests/watchlists.spec.ts` — source_type and source_uri values per test
- Read: `backend/internal/api/watchlists.go` — handler logic

**Step 1: Map each source_type to its ValidateConfig logic**

For each provider (rss_feed, spotify_playlist, spotify_liked, lastfm_loved, lastfm_top, listenbrainz_listens, discogs_wantlist, lidarr_wanted, local_file, local_directory):
- Read its `ValidateConfig()` implementation
- Determine what validations it performs (URI format, file existence, API call, etc.)
- Note which checks pass/fail in the test environment

**Step 2: Categorize fixes per source_type**

Group into:
- **File-system path needed** (local_file, local_directory) → create dirs before tests
- **URI format mismatch** → adjust test source_uri values
- **External dependency needed** (API keys not configured) → skip those source_types or add minimal configs
- **Should already pass** → verify

---

### Task 8: Fix card selectors

**Files:**
- Read: `ops/web/templates/` partials for watchlist cards — check ID format
- Modify: `e2e/tests/watchlists.spec.ts` — update selectors

**Step 1: Check template ID format**

Find the watchlist card template and check what `id` attribute is rendered (numeric vs UUID).

**Step 2: Update test selectors**

Change `#watchlist-${id}` to match actual rendered ID format. Or add `data-testid` attributes to templates.

**Step 3: Commit**

```bash
git add e2e/tests/watchlists.spec.ts
git commit -m "fix(e2e): fix watchlist card selectors to match template IDs"
```

---

### Task 9: Fix HTMX interaction timing

**Files:**
- Modify: `e2e/tests/watchlists.spec.ts`

**Step 1: Diagnose each timing failure**

For modal overlay close, sync button click, and page context close:
- Read the error context files
- Determine if the issue is missing elements (from selector mismatch) or genuine timing

**Step 2: Apply fixes**

For genuine timing issues: add `waitForHtmx` calls or increase timeouts.
For missing element issues: fixed by Task 8.

**Step 3: Commit**

```bash
git add e2e/tests/watchlists.spec.ts
git commit -m "fix(e2e): fix watchlists HTMX interaction timing"
```

---

### Task 10: Phase 2 full verification

**Step 1: Run all watchlist tests**

```bash
cd e2e && npx playwright test tests/watchlists.spec.ts
```

Expected: >= 90% pass. Accept SKIP for provider types requiring external auth.

**Step 2: Run full suite to check regressions**

```bash
cd e2e && npx playwright test
```

Expected: All Phase 1 suites still pass.

**Step 3: Push and create PR #189**

```bash
git push origin HEAD
gh pr create \
  --title "Phase 2: Watchlist E2E test fixes" \
  --body "See docs/plans/2026-07-14-e2e-remaining-issues-design.md" \
  --base main
```

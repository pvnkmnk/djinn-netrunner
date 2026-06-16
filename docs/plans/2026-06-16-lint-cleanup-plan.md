# Lint Cleanup + Context Propagation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all 91 pre-existing golangci-lint issues and 2 duplicate integration-test declarations on the `chore/mise-dev-env` branch, making CI green.

**Architecture:** Fixes grouped into 4 sections by effort and risk. Section 3 (context propagation) uses council consultation for safe refactoring. Section 4 (hard errcheck) uses council for ambiguous error-handling decisions. Each section committed separately for clean history.

**Tech Stack:** Go, golangci-lint v2, GORM, Fiber v2

**Verification:** After each task, run `golangci-lint run ./... --timeout 3m` in `backend/` to confirm 0 issues.

---

### Task 0: Verify current state

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "^" 
# Expected: 91 issues
```

---

### Task 1: Fix duplicate declarations in integration tests

**Files:**
- Modify: `backend/internal/integration/admin_smoke_test.go:15-27`

**Step 1: Remove `skipIfShort` duplicate**

Delete lines 15-19 from `admin_smoke_test.go`:
```go
// skipIfShort is copied from smoke_test.go to avoid import cycles
func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration smoke test in short mode")
	}
}
```

**Step 2: Remove `GetEnvOrDefault` duplicate**

Delete lines 22-27 from `admin_smoke_test.go`:
```go
// GetEnvOrDefault is copied from test_runner.go to avoid import cycles
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
```

**Step 3: Remove unused `os` import if it becomes unused**

Check that `"os"` import in `admin_smoke_test.go` is still needed (it is — used in the test body for INTEGRATION_BASE_URL).

**Step 4: Verify compilation**

```bash
cd backend && go vet ./internal/integration/...
# Expected: no duplicate declaration errors
```

**Step 5: Commit**

```bash
git add backend/internal/integration/admin_smoke_test.go
git commit -m "fix: remove duplicate skipIfShort and GetEnvOrDefault in integration tests"
```

---

### Task 2: Fix errcheck Close() — all 15 instances

**Files:**
- Modify: `backend/internal/services/acoustid_service.go` (1)
- Modify: `backend/internal/services/cover_art.go` (2)
- Modify: `backend/internal/services/discogs_service_test.go` (2)
- Modify: `backend/internal/services/discogs_provider_test.go` (1)
- Modify: `backend/internal/services/proxy_client_test.go` (1)
- Modify: `backend/internal/integration/admin_smoke_test.go` (many)
- Modify: `backend/internal/database/locks_test.go` (2)
- Modify: `backend/internal/database/models_test.go` (1)

**Pattern:** Every `resp.Body.Close()`, `sqlDB.Close()`, `lm.Close()` etc. with unchecked error.

**Fix type A — deferred Close in production code** (acoustid_service, cover_art):
```go
// Before:
defer resp.Body.Close()

// After:
defer func() { _ = resp.Body.Close() }()
```

**Fix type B — direct Close in test code** (most test files):
```go
// Before:
resp.Body.Close()
// After:
_ = resp.Body.Close()
```

**Fix type C — `defer lm.Close()`** (locks_test.go):
```go
defer func() { _ = lm.Close() }()
// or simply:
_ = lm.Close()  // if not deferred
```

**Step 1: Fix acoustid_service.go**

```go
// Line 67
defer func() { _ = resp.Body.Close() }()
```

**Step 2: Fix cover_art.go** — two instances of `resp.Body.Close()`, same pattern.

**Step 3: Fix all test files** — run a grep to find remaining:
```bash
cd backend && grep -rn '\.Body\.Close()' *_test.go */**/*_test.go | grep -v '_ ='
```
Replace each with `_ = resp.Body.Close()`.

**Step 4: Verify**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "errcheck"
# Expected: 35 remaining (50 - 15)
```

**Step 5: Commit**

```bash
git add -u
git commit -m "fix: handle unchecked Close() errors across services and tests"
```

---

### Task 3: Fix errcheck Setenv/Unsetenv — all 5 instances

**Files:**
- Modify: `backend/internal/config/config_test.go` (4)
- Modify: `backend/internal/services/file_watchlist_provider_test.go` (1)

**Pattern:**
```go
// Before:
os.Setenv("KEY", "val")

// After (optional, cleaner for tests because auto-restores at test end):
t.Setenv("KEY", "val")
```

**Note:** `t.Setenv` is preferred because it auto-restores the env var at test end, and avoids the `defer os.Unsetenv(...)` pattern entirely. But `t.Setenv` panics if called after the test function starts running — only use it in the test function body, not in helper functions or `TestMain`.

For cases inside test functions, use `t.Setenv`.
For `config_test.go` lines 10-13 which are `defer os.Unsetenv(...)`, switch to `t.Setenv` and remove the defer.

**Step 1: Fix config_test.go**

```go
// Before:
os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
os.Setenv("MUSICBRAINZ_API_KEY", "test-key")
defer os.Unsetenv("DATABASE_URL")
defer os.Unsetenv("MUSICBRAINZ_API_KEY")

// After:
t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
t.Setenv("MUSICBRAINZ_API_KEY", "test-key")
```

Do the same for all `os.Setenv` / `os.Unsetenv` calls in `config_test.go`.

**Step 2: Fix file_watchlist_provider_test.go**

Apply `t.Setenv` pattern.

**Step 3: Verify**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "errcheck"
# Expected: 30 remaining
go test ./internal/config/... ./internal/services/... -run TestConfig -v
# Expected: PASS
```

**Step 4: Commit**

```bash
git add -u
git commit -m "fix: use t.Setenv instead of unchecked os.Setenv in tests"
```

---

### Task 4: Remove unused code

**Files:**
- Modify: `backend/internal/api/handlers_test.go` — remove `registerAndLogin`
- Modify: `backend/internal/services/lidarr_provider.go` — remove `lidarrArtist`

**Step 1: Remove `registerAndLogin` from handlers_test.go**

Check if it's used anywhere (it shouldn't be, since linter flagged it):
```bash
cd backend && grep -rn "registerAndLogin" internal/api/
```
If truly unused:
```go
// Delete the entire function:
func registerAndLogin(t *testing.T, app *fiber.App, email, password string) string { ... }
```

**Step 2: Remove `lidarrArtist` type from lidarr_provider.go**

```go
// Delete the entire type:
type lidarrArtist struct { ... }
```

**Step 3: Verify**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "unused"
# Expected: 0
```

**Step 4: Commit**

```bash
git add -u
git commit -m "fix: remove unused registerAndLogin helper and lidarrArtist type"
```

---

### Task 5: Fix staticcheck error string capitalization (ST1005)

**Files:**
- Modify: `backend/internal/services/spotify_spdc.go` (2 instances)
- Modify: `backend/internal/services/cover_art.go` (1 instance)

**Pattern:** Staticcheck `ST1005` — error strings should not be capitalized (they're often wrapped/embedded in larger errors).

**Step 1: Fix spotify_spdc.go**

```go
// Line ~602: Before
return nil, fmt.Errorf("Web API request failed: %w", err)
// After
return nil, fmt.Errorf("web API request failed: %w", err)

// Line ~620: Before
return nil, fmt.Errorf("Web API returned HTTP %d for playlist %s", resp.StatusCode, playlistID)
// After
return nil, fmt.Errorf("web API returned HTTP %d for playlist %s", resp.StatusCode, playlistID)
```

**Step 2: Fix cover_art.go**

Lowercase the error string there too.

**Step 3: Verify**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "ST1005"
# Expected: 0
```

**Step 4: Commit**

```bash
git add -u
git commit -m "fix: lowercase error strings per Go conventions (ST1005)"
```

---

### Task 6: Fix SA1012 — nil Context → context.Background()/TODO()

**Files:**
- Modify: `backend/internal/services/safe_http.go`
- Modify: `backend/internal/services/acoustid_service.go`
- Modify: `backend/internal/services/disk_quota_service.go`

**Pattern:** Passing `nil` as Context to functions that expect `context.Context`. Replace with `context.Background()` (call already in top-level function) or `context.TODO()` (placeholder for future context threading).

**Step 1: Fix safe_http.go**

Find the nil Context usage and replace with `context.Background()`.

**Step 2: Fix acoustid_service.go**

Same pattern.

**Step 3: Fix disk_quota_service.go**

Same pattern.

**Step 4: Verify**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "SA1012"
# Expected: 0
```

**Step 5: Commit**

```bash
git add -u
git commit -m "fix: replace nil Context with context.Background() (SA1012)"
```

---

### Task 7: Fix QF1008 — remove embedded Dialector field

**Files:**
- Modify: files flagged by golangci-lint with `QF1008`

**Pattern:** Embedded struct field that could be removed — the struct method set is promoted anyway.

Run to locate:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep "QF1008"
```

Each fix is specific to the struct. Usually just remove the embedded field name (keep the type), or inline it.

**Step 1-3:** Fix each occurrence.

**Step 4: Verify + Commit**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "QF1008"
# Expected: 0
git add -u && git commit -m "fix: remove redundant embedded struct fields (QF1008)"
```

---

### Task 8: Fix QF1003 — tagged switches

**Files:**
- Modify: files flagged by golangci-lint with `QF1003`

**Pattern:** `if/else` chains on type assertion that could be a `switch x.(type)`.

Run to locate:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep "QF1003"
```

Quick fix example:
```go
// Before:
if page, ok := x.(Page); ok { return page }
if tid, ok := x.(TrackID); ok { ... }

// After:
switch v := x.(type) {
case Page:
    return v
case TrackID:
    ...
}
```

**Step 1-4:** Fix each occurrence (likely in handlers.go, worker/assigner.go, watchlist/track_parser.go).

**Step 5: Verify + Commit**

---

### Task 9: Fix remaining staticcheck singletons

**Files:**
- Modify: files flagged by golangci-lint

Run to locate each:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -E "SA1019|QF1001|S1023|SA9003|S1021|S1009"
```

**Fixes (1 each):**

| Linter | Fix |
|---|---|
| SA1019 | Replace deprecated `c.Request().Header.VisitAll` with Fiber v2 equivalent |
| QF1001 | Apply De Morgan's law to simplify boolean expression |
| S1023 | Remove redundant `return` at end of function |
| SA9003 | Implement or remove empty `if` block with comment |
| S1021 | `var x T; x = val` → `x := val` |
| S1009 | Remove `x != nil` check when `len(x) > 0` already handles nil slice |

**Step 1-6:** Fix each one.

**Step 7: Verify + Commit**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "SA1019\|QF1001\|S1023\|SA9003\|S1021\|S1009"
# Expected: 0
git add -u && git commit -m "fix: miscellaneous staticcheck issues"
```

---

### Task 10: Fix ineffassign — 4 unused assignments

**Files:**
- Modify: files flagged by golangci-lint

Run to locate:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep "ineffassign"
```

Each fix: either remove the dead assignment, or add a usage if the value matters.

**Step 1-4:** Fix each occurrence.

**Step 5: Verify + Commit**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "ineffassign"
# Expected: 0
git add -u && git commit -m "fix: remove ineffective assignments"
```

---

### Task 11: Context propagation — full refactor (16 noctx issues)

**Files:**
- Modify: `backend/internal/services/safe_http.go`
- Modify: `backend/internal/services/acoustid_service.go`
- Modify: `backend/internal/services/disk_quota_service.go`
- Modify: other files flagged by noctx

**IMPORTANT: Before implementing, consult @council[architect] for the context propagation design.**

Run to locate all:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep "noctx\|net/http.Client\|database/sql.DB\|net.Resolver"
```

**Council prompts:**

For `safe_http.go` (most complex — involves `*net/http.Client`, `*net.Resolver`, and HTTP requests):
```
@council[architect] I need to add context.Context propagation through safe_http.go in NetRunner (Go/Fiber). Current code uses:
- Zero-value http.Client (no timeout/context)
- net.Resolver{} without context
- http.NewRequest without context

The linter (noctx) wants context propagation. What's the minimal, safe pattern to:
1. Create http.Client with timeout and context
2. Pass context through the resolver
3. Use http.NewRequestWithContext

The functions involved are: [exact function signatures]. Give me the implementation.
```

For `disk_quota_service.go` (GORM context):
```
@council[architect] In NetRunner, disk_quota_service.go uses raw *sql.DB queries without context. GORM supports db.WithContext(ctx). What's the cleanest pattern to thread a context through these quota check functions?
```

**Step 1: Council → design safe_http.go context propagation**

**Step 2: Implement safe_http.go changes**

**Step 3: Council → design acoustid_service.go fixes**

**Step 4: Implement acoustid_service.go changes**

**Step 5: Council → design disk_quota_service.go fixes**

**Step 6: Implement disk_quota_service.go changes**

**Step 7: Fix remaining noctx issues** — apply the same pattern.

**Step 8: Verify + Commit**

```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep -c "noctx"
# Expected: 0
cd backend && go build ./...
# Expected: compiles clean
git add -u && git commit -m "feat: add context propagation to HTTP/DB calls"
```

---

### Task 12: Fix hard errcheck — 30 non-Close issues

**Files:** Various — run to locate:
```bash
cd backend && golangci-lint run ./... --timeout 3m 2>&1 | grep "errcheck" | grep -v "Close\|Setenv\|Unsetenv"
```

**Common patterns and fixes:**

| Pattern | Count | Fix |
|---|---|---|
| `database.Migrate(db)` in tests | ~7 | `if err := database.Migrate(db); err != nil { t.Fatalf(...) }` |
| `lm.ReleaseLock(...)` | ~3 | `_ = lm.ReleaseLock(...)` (low-risk, for cleanup in tests) |
| `db.Save(...)` / `db.Create(...)` | ~5 | Check error and return/log it |
| `os.Remove(...)` | ~3 | `_ = os.Remove(...)` or check error |
| Other I/O | ~8 | Case-by-case error handling |
| Other misc | ~4 | Council if ambiguous |

**For ambiguous cases, consult @council:**
```
@council I need to fix an unchecked error in Go. The function is [name], and the unchecked call is [call]. What's the right way to handle this error — should I propagate it, log it, or use _ = ?
```

**Step 1-30:** Fix each issue.

**Step 31: Final verification**

```bash
cd backend && golangci-lint run ./... --timeout 3m
# Expected: 0 issues
```

**Step 32: Commit**

```bash
git add -u && git commit -m "fix: handle unchecked errors across codebase"
```

---

### Task 13: Total verification

**Step 1: Zero lint issues**
```bash
cd backend && golangci-lint run ./... --timeout 3m
# Expected: no output (clean)
```

**Step 2: Go vet**
```bash
cd backend && go vet ./...
# Expected: no output (clean)
```

**Step 3: Build all binaries**
```bash
cd backend && go build ./cmd/...
# Expected: compiles clean
```

**Step 4: Run core tests**
```bash
cd backend && go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent
# Expected: PASS
```

**Step 5: Push and verify CI**
```bash
git push origin chore/mise-dev-env
```
Check PR #171 CI — expect `test` ✅, `integration` ✅, all others ✅.

**Step 6: Final commit note**

If CI is green and CodeRabbit hasn't reviewed yet, trigger with:
```
@coderabbitai review
```
(as a PR comment, when rate limit is available)

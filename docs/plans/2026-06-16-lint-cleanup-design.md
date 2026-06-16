# Lint Cleanup + Context Propagation Design

> **Status:** Design approved, awaiting implementation plan
> **PR:** chore/mise-dev-env (#171)
> **Goal:** Fix all 91 pre-existing golangci-lint issues + 2 duplicate integration-test declarations

## Problem

After migrating `.golangci.yml` to v2 format, golangci-lint revealed 91 pre-existing issues in the codebase that were never caught (no linter CI ran on master). Two `package integration` files also have duplicate function declarations that cause compilation failures under the `integration` build tag.

## Scope

Fix **all** 91 lint issues and **both** duplicates in a single pass using the `chore/mise-dev-env` branch.

## Architecture / Approach

Fixes grouped into 4 sections by effort and risk, applied in dependency order:

### Section 1: Duplicates + Trivial (22 issues)

| Issue | Count | Pattern | Files |
|---|---|---|---|
| Duplicate `skipIfShort` | 1 | Delete from `admin_smoke_test.go` (already in `smoke_test.go`, same package) | `admin_smoke_test.go` |
| Duplicate `GetEnvOrDefault` | 1 | Delete from `admin_smoke_test.go` (already in `test_runner.go`, same package) | `admin_smoke_test.go` |
| `errcheck: Close()` | 15 | `_ = resp.Body.Close()` | `acoustid_service.go`, `cover_art.go`, various `*_test.go` |
| `errcheck: Setenv/Unsetenv` | 5 | `_ = os.Setenv(...)` or `t.Setenv(...)` | `config_test.go`, `file_watchlist_provider_test.go` |
| `unused` | 2 | Remove `registerAndLogin`, `lidarrArtist` | `handlers_test.go`, `lidarr_provider.go` |

### Section 2: Staticcheck + ineffassign (23 issues)

| Issue | Count | Pattern |
|---|---|---|
| ST1005 error strings capitalized | 3 | Lowercase first letter |
| SA1012 nil Context | 3 | `context.Background()` / `context.TODO()` |
| QF1008 embedded field | 3 | Remove or inline embedded field |
| QF1003 tagged switch | 4 | Convert `if/else` on type to `switch x.(type)` |
| SA1019 deprecated call | 1 | Replace `c.Request().Header.VisitAll` in Fiber |
| QF1001 De Morgan's law | 1 | Simplify boolean expression |
| S1023 redundant return | 1 | Remove unnecessary `return` |
| SA9003 empty branch | 1 | Implement or remove empty `if` block |
| S1021 merge declaration | 1 | `var x T; x = val` → `x := val` |
| S1009 nil-slice check | 1 | Remove redundant nil check |
| ineffassign | 4 | Remove dead assignments |

### Section 3: Context propagation (16 issues)

Full refactor to add `context.Context` through HTTP calls, DB queries, and resolver lookups:

- `acoustid_service.go`: Use `http.NewRequestWithContext`
- `safe_http.go`: Thread context through HTTP client, net.Resolver
- `disk_quota_service.go`: Use `db.WithContext(ctx)`
- Various files: Standard noctx pattern → `http.NewRequestWithContext`

Council consultation will be used to verify the context propagation design before implementation.

### Section 4: Hard errcheck (30 issues)

Non-trivial error returns needing judgment calls. Patterns:

- `Migrate(db)` in tests: Add error-aware helpers
- `lm.ReleaseLock(...)`: Log-based error handling
- `db.Save/Create`: Return or log errors
- File I/O: Propagate or handle

Council consultation for ambiguous cases.

## Files Touched

~25 files across:
- `backend/internal/integration/` (duplicates)
- `backend/internal/services/` (bulk of fixes)
- `backend/internal/database/` (test fixes)
- `backend/internal/config/` (test fixes)
- `backend/internal/api/` (unused code, staticcheck)
- `backend/cmd/worker/` (staticcheck)
- `.golangci.yml` (v2 migration already done)

## Verification

After all fixes:
1. `cd backend && go vet ./...`
2. `cd backend && golangci-lint run ./... --timeout 3m` — 0 issues
3. `cd backend && go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent`
4. Push, CI re-runs — both `test` and `integration` checks should pass

## Risk Assessment

| Area | Risk | Mitigation |
|---|---|---|
| errcheck Close/Setenv | None | Purely cosmetic (`_ =` prefix) |
| Staticcheck | Low | Well-understood Go idioms |
| Ineffassign | Low | Removing dead code |
| Context propagation | Medium | Full refactor via council review |
| Hard errcheck | Low-Medium | Council for ambiguous cases |
| Duplicate removal | None | Same-package visibility |

## Design Decisions

1. **t.Setenv over `_ =`**: Prefer `t.Setenv(key, val)` in test files — it's idiomatic and auto-restores env at test end.
2. **Context propagation boundaries**: Contexts will be created at the call site (not threaded through public API signatures) where the linter only flags local usage. Where function signatures need widening, a minimal `ctx context.Context` parameter will be added.
3. **Council triggers**: Context propagation design (Section 3) + any errcheck fix where the right handling is ambiguous.

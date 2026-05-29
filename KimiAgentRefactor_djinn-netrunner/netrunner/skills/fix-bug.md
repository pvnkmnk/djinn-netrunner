***
skill: fix-bug
version: 1
repo: netrunner
language: go
tags: [debugging, bugfix, regression, testing]
***

# Skill: Fix Bug

## Purpose
Reproduce, isolate, patch, and verify bugs in NetRunner without regressions.

## Prerequisites
- Repro details (endpoint, command, job type, error logs).
- Ability to run relevant package tests.

## Core Concepts
- Most failures are traceable along handler -> service -> DB transitions.
- Worker bugs often involve job state transitions, locking, or heartbeat/zombie cleanup.
- Logging is structured with `slog`, and job logs are persisted in `joblogs`.

## Step-by-Step Procedures
1. Reproduce with narrow command.
```bash
cd backend
go test ./internal/<package> -run <TestName> -v
```
2. Trace ownership/auth checks for API bugs.
3. Trace transaction/state transitions for worker/job bugs.
4. Patch smallest unit that fixes the failure.
5. Add regression test near the changed behavior.
6. Re-run focused tests, then broader suite.
```bash
go test ./internal/<package> -v
go test ./cmd/<related-cmd> -v
```

## Code Patterns
Ownership-safe query pattern:
```go
query := db.Where("id = ?", id)
if user.Role != "admin" {
    query = query.Where("owner_user_id = ?", user.ID)
}
```

## Validation
- Repro test fails before patch and passes after patch.
- No new failures in adjacent package tests.
- Logs no longer emit the original error path in equivalent scenario.

## Edge Cases & Error Handling
- Do not silently widen access when fixing authorization bugs.
- Preserve idempotency for enqueue/retry/cancel operations.
- When failure is environment-specific (e.g., Windows path semantics), document scope explicitly.

## References
- `backend/internal/api/`
- `backend/internal/services/job_handlers.go`
- `backend/internal/database/locks.go`
- `backend/cmd/worker/main.go`
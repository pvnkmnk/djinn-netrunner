***
skill: add-feature
version: 1
repo: netrunner
language: go
tags: [feature, workflow, api, services, database]
***

# Skill: Add Feature

## Purpose
Implement a new feature in NetRunner with consistent layering, tests, and operational safety.

## Prerequisites
- Local branch with clean working state.
- Baseline package tests runnable for affected modules.

## Core Concepts
- Flow is handler -> service -> database model/query.
- New core workflows should remain available via HTTP/CLI/MCP where appropriate.
- Use `log/slog` for structured operational logs.

## Step-by-Step Procedures
1. Identify surface area.
```text
HTTP route? -> backend/internal/api
Business logic? -> backend/internal/services
Persistence? -> backend/internal/database
Agent/CLI exposure? -> backend/internal/agent + backend/cmd/{agent,cli}
```
2. Add model/schema changes first (if needed).
3. Implement service logic with minimal API coupling.
4. Add/modify handler routes and payload validation.
5. Wire CLI and/or MCP tools if feature is operator-facing.
6. Add tests at the smallest useful layer first, then handler integration tests.
7. Validate.
```bash
cd backend
go vet ./...
go test ./internal/<affected-package> -v
go test ./cmd/<affected-cmd> -v
```

## Code Patterns
Fiber handler payload validation:
```go
var payload struct {
    Name string `json:"name"`
}
if err := c.BodyParser(&payload); err != nil {
    return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
}
if payload.Name == "" {
    return c.Status(400).JSON(fiber.Map{"error": "name is required"})
}
```

## Validation
- New behavior is covered by tests in touched package(s).
- Auth and ownership checks are preserved on protected routes.
- `go build` succeeds for relevant cmd binaries.

## Edge Cases & Error Handling
- Avoid bypassing ownership filters (`owner_user_id`) for non-admin users.
- Ensure new job types/params remain compatible with worker orchestration and locks.
- For external APIs, use existing safe/rate-limited clients instead of ad hoc HTTP calls.

## References
- `backend/cmd/server/main.go`
- `backend/internal/api/`
- `backend/internal/services/`
- `backend/internal/database/models.go`
- `backend/cmd/agent/main.go`
- `backend/cmd/cli/main.go`
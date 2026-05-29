***
skill: api-usage
version: 1
repo: netrunner
language: go
tags: [api, cli, mcp, auth]
***

# Skill: API and Interface Usage

## Purpose
Use NetRunner's HTTP API, CLI, and MCP interfaces correctly with session auth and ownership constraints.

## Prerequisites
- NetRunner server running.
- User account/session established for protected endpoints.

## Core Concepts
- Auth model is cookie session (`session_id`), not bearer JWT.
- Most `/api/*` routes are under `AuthMiddleware`.
- CLI and MCP map to `internal/agent` functions for shared behavior.

## Step-by-Step Procedures
1. Register/login through auth endpoints.
```bash
curl -i -X POST http://localhost:8080/api/auth/register -H "Content-Type: application/json" -d '{"email":"user@example.com","password":"secret"}'
curl -i -c cookies.txt -X POST http://localhost:8080/api/auth/login -H "Content-Type: application/json" -d '{"email":"user@example.com","password":"secret"}'
```
2. Call protected endpoint with cookie jar.
```bash
curl -b cookies.txt http://localhost:8080/api/watchlists/
```
3. Use CLI for operational workflows.
```bash
cd backend
go run ./cmd/cli status
go run ./cmd/cli watchlist list
```
4. Use MCP tools via `backend/cmd/agent` transport when agent integration is required.

## Code Patterns
Session auth guard:
```go
sessionID := c.Cookies("session_id")
if sessionID == "" {
    return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
}
```

## Validation
- Protected API returns `401` without cookie and success with valid cookie.
- CLI commands return structured output and non-zero on failure.
- MCP tool calls map to expected DB mutations/actions.

## Edge Cases & Error Handling
- Non-admin users are filtered by `owner_user_id`; do not expect global listings.
- OAuth Spotify callback requires matching state cookie and query state.
- Some endpoints return HTML partials for HTMX instead of JSON; inspect route intent.

## References
- `backend/cmd/server/main.go`
- `backend/internal/api/auth.go`
- `backend/internal/api/spotify_auth.go`
- `backend/cmd/cli/main.go`
- `backend/cmd/agent/main.go`
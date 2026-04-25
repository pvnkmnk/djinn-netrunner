# netrunner Agent Rules

## Project overview
netrunner is a Soulseek file management and automation system.
Stack: Go, Fiber, PostgreSQL, Redis, Docker/Compose, HTMX/Jinja2, JWT+RBAC, self-host-ready.

## Module ownership
- `backend/cmd/` — CLI entrypoints (agent, cli, server, worker)
- `backend/internal/api/` — Fiber HTTP handlers and routes
- `backend/internal/services/` — Business logic services (slskd, transcoder, etc.)
- `backend/internal/database/` — GORM models and queries
- `backend/internal/agent/` — MCP server implementation
- `ops/` — Docker Compose, Nginx, PostgreSQL configs

## Agent role assignments
- **build** → feature implementation, routes, database migrations
- **plan** → RFC writing, schema design, API contracts
- **review** → security audit, RBAC/JWT checks, PR feedback
- **infra** → Docker, Postgres, Redis, deployment
- **netrunner** → research, library eval, CVE lookup

## Coding standards
- Use `log/slog` for structured logging (never `log` or `fmt` for output)
- All endpoints require JWT/session auth unless marked `@public`
- RBAC: roles defined in `backend/internal/database/`; never hardcode role names
- Migrations: use GORM AutoMigrate or add SQL migrations in `ops/db/init/migrations/`
- All secrets via environment variables or `.env` (never committed)
- Tests in `backend/internal/*/*_test.go`; run `go test ./...` before any PR
- Use `gorm.io/gorm` for database operations; never raw `database/sql`

## Security
- Validate all inputs with strict type checking
- Rate-limit all public endpoints via Redis
- Audit log all state-changing operations
- Use `filepath.Rel` for path traversal validation (never string prefix checks)

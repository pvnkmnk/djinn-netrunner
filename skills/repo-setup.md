***
skill: repo-setup
version: 1
repo: netrunner
language: go
tags: [setup, environment, docker, go, database]
***

# Skill: Repository Setup

## Purpose
Set up NetRunner from a clean clone to a runnable local environment with baseline validation.

## Prerequisites
- Git, Go 1.25+, Docker, Docker Compose installed.
- Access to Soulseek credentials for full slskd behavior.
- Write access to create `.env` in repo root.

## Core Concepts
- Runtime config is loaded from environment via `backend/internal/config/config.go`.
- `database.Migrate(db)` runs automatically on server startup.
- The container entrypoint starts both `netrunner-worker` and `netrunner-server`.

## Step-by-Step Procedures
1. Clone and enter repository.
```bash
git clone <repo-url>
cd netrunner
```
2. Create local environment file.
```bash
cp .env.example .env
```
3. Populate minimum variables in `.env`.
```env
DATABASE_URL=postgresql://musicops:<password>@localhost:5432/musicops?sslmode=disable
JWT_SECRET=<long-random-secret>
SLSKD_API_KEY=<slskd-api-key>
POSTGRES_PASSWORD=<postgres-password>
SLSKD_USERNAME=<soulseek-user>
SLSKD_PASSWORD=<soulseek-password>
```
4. Install Go dependencies.
```bash
cd backend
go mod download
```
5. Validate codebase baseline.
```bash
go vet ./...
go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent
```
6. Start full stack with Docker.
```bash
cd ..
docker compose up -d --build
```
7. Verify health endpoint.
```bash
curl http://localhost:8080/api/health
```

## Code Patterns
Use config loader instead of direct env parsing in new app code.
```go
cfg, err := config.Load()
if err != nil {
    slog.Error("config load failed", "error", err)
    os.Exit(1)
}
```

## Validation
- `docker compose ps` shows `netrunner`, `netrunner-slskd`, and `netrunner-postgres` running.
- `curl http://localhost:8080/api/health` returns `{"status":"ok"}`.
- `cd backend && go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent` succeeds.

## Edge Cases & Error Handling
- If `DATABASE_URL` is empty, startup fails fast (`DATABASE_URL is required`).
- If `JWT_SECRET` is unset, app auto-generates one (non-persistent sessions across restarts).
- If slskd auth vars are missing, acquisition functionality is degraded.

## References
- `backend/internal/config/config.go`
- `backend/cmd/server/main.go`
- `backend/internal/database/migrate.go`
- `docker-compose.yml`
- `backend/entrypoint.sh`
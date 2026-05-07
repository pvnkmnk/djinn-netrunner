***
skill: deploy
version: 1
repo: netrunner
language: go
tags: [deploy, docker, caddy, operations]
***

# Skill: Deploy NetRunner

## Purpose
Build, package, deploy, and verify NetRunner using the Docker Compose stack.

## Prerequisites
- Docker and Docker Compose installed.
- `.env` populated with production-safe secrets and credentials.
- DNS/domain ready if using public TLS.

## Core Concepts
- `backend/Dockerfile` builds `netrunner-server` and `netrunner-worker` binaries.
- Compose stack includes `postgres`, `slskd`, `gonic`, `caddy`, and `netrunner`.
- Caddy handles TLS termination and reverse proxy routing.

## Step-by-Step Procedures
1. Validate config files.
```bash
docker compose config
```
2. Build and deploy services.
```bash
docker compose up -d --build
```
3. Confirm service health.
```bash
docker compose ps
curl http://localhost:8080/api/health
```
4. Check logs for startup/migration issues.
```bash
docker compose logs -f netrunner
```
5. Validate external paths:
   - `/music/*` and `/rest/*` should reverse proxy to gonic via Caddy.

## Code Patterns
Compose env interpolation example:
```yaml
environment:
  DATABASE_URL: postgresql://musicops:${POSTGRES_PASSWORD}@postgres:5432/musicops?sslmode=disable
```

## Validation
- `netrunner` container is healthy and serving `/api/health`.
- Worker logs show heartbeat loop and job polling startup.
- Caddy serves expected domain and proxies routes correctly.

## Edge Cases & Error Handling
- If `JWT_SECRET` is missing, server still starts but sessions may reset across restart.
- If `SLSKD_API_KEY` is missing, acquisition/search endpoints degrade.
- Rollback: redeploy last known-good image tag or previous compose revision.

## References
- `backend/Dockerfile`
- `docker-compose.yml`
- `ops/caddy/Caddyfile`
- `backend/entrypoint.sh`
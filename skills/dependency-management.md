***
skill: dependency-management
version: 1
repo: netrunner
language: go
tags: [dependencies, security, modules, maintenance]
***

# Skill: Dependency Management

## Purpose
Add, update, audit, and troubleshoot Go dependencies in NetRunner safely.

## Prerequisites
- Go toolchain available.
- Ability to run test/build commands in `backend/`.

## Core Concepts
- Source of truth is `backend/go.mod` and `backend/go.sum`.
- CI validates with `go vet` + `go test`.
- Security scanning should include reachable-vulnerability checks with `govulncheck`.

## Step-by-Step Procedures
1. Inspect current module state.
```bash
cd backend
go list -m all
```
2. Add or upgrade a dependency.
```bash
go get <module>@<version>
go mod tidy
```
3. Build and test after updates.
```bash
go vet ./...
go test ./...
go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent
```
4. Run vulnerability audit.
```bash
govulncheck ./...
```
5. If lockfile conflicts occur in `go.sum`, rerun tidy and resolve by re-fetching exact versions.

## Code Patterns
Pinning a module version:
```bash
go get github.com/gofiber/fiber/v2@v2.52.12
```

## Validation
- `go.mod` and `go.sum` are internally consistent (`go mod tidy` clean).
- Vet, tests, and builds pass for affected surfaces.
- `govulncheck` findings are reviewed and triaged.

## Edge Cases & Error Handling
- When tests fail after upgrade, bisect by pinning previous module versions.
- Prefer minimal version bumps for high-risk dependencies touching auth/job orchestration.
- Treat integration failures separately from module issues when external services are unavailable.

## References
- `backend/go.mod`
- `.github/workflows/ci.yml`
- Context7 Go docs: `govulncheck ./...` usage
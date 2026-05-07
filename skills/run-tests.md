***
skill: run-tests
version: 1
repo: netrunner
language: go
tags: [testing, go-test, integration, ci]
***

# Skill: Run Tests

## Purpose
Execute NetRunner test suites efficiently, from narrow regression checks to full CI-equivalent runs.

## Prerequisites
- Dependencies installed (`go mod download`).
- For integration tests: Docker daemon running.

## Core Concepts
- Unit and package tests use standard `go test` with `*_test.go` conventions.
- Integration suite is gated by build tag `integration`.
- CI currently runs `go vet` and `go test ./... -coverprofile=coverage.out` from `backend/`.

## Step-by-Step Procedures
1. Run static checks.
```bash
cd backend
go vet ./...
```
2. Run full default suite.
```bash
go test ./...
```
3. Run core passing suite (current workspace baseline).
```bash
go test ./cmd/... ./internal/config ./internal/database ./internal/services ./internal/agent
```
4. Run a single package.
```bash
go test ./internal/api -v
```
5. Run a single test case.
```bash
go test ./internal/api -run TestAuthMiddleware_InvalidSession -v
```
6. Run integration tests directly.
```bash
go test ./internal/integration/... -tags=integration -v -timeout 10m
```
7. Run integration tests via helper script.
```bash
cd ..
./scripts/integration-tests.sh test
```

## Code Patterns
Regression test naming convention:
```go
func TestFeatureName_Scenario(t *testing.T) {
    // arrange
    // act
    // assert
}
```

## Validation
- `go vet ./...` returns clean output.
- Target package/test commands return exit code 0.
- Integration script reports services healthy before test execution.

## Edge Cases & Error Handling
- Known issue (2026-05-07): `go test ./...` currently fails in `internal/api/libraries_test.go` on path-expectation mismatch in this workspace.
- Use package-scoped runs to isolate failures before broad reruns.
- Set `SKIP_INTEGRATION_TESTS=true` to bypass integration suite in constrained environments.

## References
- `.github/workflows/ci.yml`
- `scripts/integration-tests.sh`
- `backend/internal/integration/slskd_integration_test.go`

---
name: release-readiness-review
description: "Two-phase pattern for release readiness: Phase 1 audits build/vet/test/security/docs → report gaps. Phase 2 closes gaps and tags the release."
---

# Release Readiness Review

## Overview

Two-phase pattern for releasing NetRunner. Phase 1 audits everything and produces a gap report. Phase 2 closes all gaps and tags the release.

## Phase 1: Audit

Run all checks and report findings. Do NOT fix anything in this phase.

### Checklist

- [ ] **Git state**: branch name, commit count from master, uncommitted changes
- [ ] **Build**: `go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent` — all pass
- [ ] **Vet**: `go vet ./...` — all clean
- [ ] **Tests**: `go test ./cmd/... ./internal/config/... ./internal/database/... ./internal/services/... ./internal/agent/... -count=1`
  - Report count: `N passed, M failed`
  - Flag pre-existing failures (test on master first to compare)
- [ ] **Vulnerabilities**: `govulncheck ./...` — 0 reachable
- [ ] **Documentation**: CHANGELOG, README, AGENTS.md, ARCHITECTURE.md, RUNBOOK.md, ADRs
- [ ] **CI/CD**: `.github/workflows/` — verify CI, docker, integration workflows
- [ ] **Security**: session auth, BOLA, CSRF, CSP, XSS escaping, SSRF protection
- [ ] **Docker**: compose file with healthchecks, Dockerfile correctness

### Output

Produce a gap report: what's ready now vs what needs fixing before the tag.

## Phase 2: Gap Closure

Implement all gaps found in Phase 1, then tag the release.

### Typical Gaps

1. **Test infrastructure**: Integration-tagged tests excluded from unit CI, integration CI workflow
2. **Documentation**: CHANGELOG entry, env vars in .env.example
3. **E2E smoke tests**: Auth, watchlist, library, job, webhook, quota, admin
4. **Accessibility**: Mobile nav, keyboard nav, focus management, skip-to-content
5. **Admin panel**: User CRUD, audit log, config editor
6. **Infrastructure**: Docker image quality (yt-dlp version, Node.js), multi-env config, LiteFS support

### Tagging

```bash
git tag -a v0.0.1 -m "NetRunner v0.0.1 - Initial release"
git push origin v0.0.1

# Optionally create a PR for the release branch
gh pr create --title "Release v0.0.1" --body "<summary>"
```

### Docker Verification

```bash
docker compose build netrunner
docker compose up -d
./scripts/smoke-test.sh
```

## Related Files

- Design doc: `docs/plans/YYYY-MM-DD-release-and-gap-closure-design.md`
- Implementation plan: `docs/plans/YYYY-MM-DD-release-implementation-plan.md`
- CHANGELOG: `CHANGELOG.md`
- CI workflows: `.github/workflows/ci.yml`, `.github/workflows/integration.yml`, `.github/workflows/docker.yml`

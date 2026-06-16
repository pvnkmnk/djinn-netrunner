# Port Mise Dev Environment — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Port mise.toml, mise-based CI, golangci-lint config, and .ignore to a clean branch off master.

**Architecture:** 5 files, no dependencies, no code changes. Straight file-port from feature branch commits to a new branch off master.

**Tech Stack:** Go, mise, GitHub Actions, golangci-lint

---

### Task 1: Create branch and port files

**Files:**
- Create: `mise.toml`
- Create: `.golangci.yml`
- Create: `.ignore`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/docker.yml`

**Step 1: Create new branch from master**

Run: `git checkout master && git pull --ff-only && git checkout -b chore/mise-dev-env`

**Step 2: Port files from feature branch commits**

The 5 files were introduced by two commits:
- `1209b69` — CI commit (introduced ci.yml changes, docker.yml changes)
- `ebac6b4` — massive commit (introduced mise.toml, .golangci.yml, .ignore)

Copy each file from the old branch:
```bash
git checkout feature/muserve-coolify-integration -- mise.toml .golangci.yml .ignore
git checkout feature/muserve-coolify-integration -- .github/workflows/ci.yml .github/workflows/docker.yml
```

**Step 3: Verify only these 5 files changed**

Run: `git diff --stat master`
Expected: Only the 5 files listed, with no other modifications.

**Step 4: Verify master source code integrity**

Run: `ls backend/internal/api/acquire.go backend/internal/api/admin_handler.go backend/internal/api/litefs_middleware.go backend/internal/metrics/`
Expected: All files exist (ensuring none were accidentally deleted)

**Step 5: Verify build still works**

Run: `cd backend && go vet ./... && go build ./cmd/...`
Expected: Clean pass, no compilation errors.

**Step 6: Commit**

```bash
git add mise.toml .golangci.yml .ignore .github/workflows/ci.yml .github/workflows/docker.yml
git commit -m "feat: add mise dev environment and optional CI tooling

- mise.toml: dev environment config (tools, tasks, quality gates)
- .golangci.yml: linter configuration for golangci-lint
- .ignore: ignore patterns for cloned dependency workflow
- ci.yml: mise-based CI pipeline (replaces raw Go commands)
- docker.yml: unpinned action versions for auto-updates"
```

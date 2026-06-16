# Port Mise Dev Environment to Master

## Goal

Port the mise-based dev environment and CI configuration from the abandoned `feature/muserve-coolify-integration` branch to a new clean branch off `master`, without any of the Coolify-specific changes or destructive deletions that made the old branch broken.

## Files to Port

| File | Source | Reason |
|---|---|---|
| `mise.toml` | feature branch | Dev environment: tools, tasks, env, quality gates. Opt-in only. |
| `.github/workflows/ci.yml` | feature branch (mise-based version) | CI pipeline using mise tasks instead of raw Go commands |
| `.github/workflows/docker.yml` | feature branch (unpinned actions) | Minor improvement: unpinned action versions for auto-updates |
| `.golangci.yml` | feature branch | Linter config referenced by mise `lint-go` task |
| `.ignore` | feature branch | Gitignore patterns for cloned dependency workflow |

## Non-Goals

- No Coolify integration (docker-compose, caddy, proxy, worker concurrency)
- No feature stripping (LiteFS, metrics, admin, acquire — all stay intact)
- No workspace snapshot cruft (codemap.md files, editor configs, docs rewrites)
- No CI workflow deletions (integration.yml, pr-sentry.yml, prguard.yml stay as-is)

## Approach

1. Create new branch `chore/mise-dev-env` off `master`
2. Copy the 5 files from feature branch using `git checkout <commit> -- <file>` for the specific commits that introduced them
3. Verify the branch is clean (only these 5 files changed)
4. PR into master

## Verification

- `git diff master --stat` shows only the 5 intended files
- `go vet ./...` and `go build ./cmd/...` pass unchanged
- `mise run validate` works (if mise is installed)

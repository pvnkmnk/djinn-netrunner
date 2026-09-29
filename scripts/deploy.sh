#!/usr/bin/env bash
# NetRunner deploy.
#
# Brings the stack up with the image stamped by the version of the tag you are
# on, instead of whatever APP_VERSION happened to be exported in the shell:
#
#   ./scripts/deploy.sh --beta                # base stack + beta overlay
#   ./scripts/deploy.sh --beta --profile media-server
#   ./scripts/deploy.sh                       # base stack only
#
# Why this exists: the composed image tag is
# `djinn-netrunner-ops-{web,worker}:${APP_VERSION}`, so the version has to be
# decided *before* compose builds. Left to the shell, undeclared is the default
# state — an operator who forgets the export builds a `:dev` image, or reuses
# whichever tag happens to exist. Deriving it from the checked-out tag means the
# tag you deploy is the version you build, and a release build and a dev build
# can never occupy the same image tag.
#
# The resolved version is written back to `.env` (a single `APP_VERSION=` line,
# replaced in place) so `scripts/beta-smoke.sh` — which reads the declared
# version from there — checks against the same value the image was built with.
#
# Version resolution, first match wins:
#   1. `APP_VERSION` in the environment — an explicit override, for building an
#      untagged commit (e.g. CI).
#   2. the tag pointing at HEAD (`git describe --tags --exact-match`).
#   3. `dev` — deliberately not a release number, so an untagged build reports
#      `NetRunner vdev` and can never masquerade as a release.
#
# Arguments:
#   --beta       also apply docker-compose.beta.yml (production overlay)
#   <anything>   passed through to `docker compose up`, e.g. --profile
#
# Environment:
#   APP_VERSION  explicit override of the derived version

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

BETA=0
EXTRA=()
for arg in "$@"; do
    case "$arg" in
        --beta) BETA=1 ;;
        -h|--help) awk 'NR == 1 { next } /^set -euo pipefail/ { exit } { sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) EXTRA+=("$arg") ;;
    esac
done

# 1. Honour an explicit override, 2. take the checked-out tag, 3. fall back to
# the dev sentinel. `describe --exact-match` fails (non-zero) off a tag, which
# is the common case during development.
if [ -z "${APP_VERSION:-}" ]; then
    if APP_VERSION="$(git describe --tags --exact-match HEAD 2>/dev/null)"; then
        :
    else
        APP_VERSION="dev"
    fi
fi
export APP_VERSION

# Persist it where `docker compose` (for ${APP_VERSION} substitution and as the
# image tag) and scripts/beta-smoke.sh (for the declared-version check) both
# read it. Idempotent: an existing APP_VERSION line is replaced, not appended.
ENV_FILE="$REPO_ROOT/.env"
if [ -f "$ENV_FILE" ]; then
    ENV_TMP="$ENV_FILE.deploy.tmp"
    awk -v v="$APP_VERSION" '
        /^APP_VERSION=/ && !set { print "APP_VERSION=" v; set = 1; next }
        { print }
        END { if (!set) print "APP_VERSION=" v }
    ' "$ENV_FILE" > "$ENV_TMP"
    mv "$ENV_TMP" "$ENV_FILE"
else
    printf 'APP_VERSION=%s\n' "$APP_VERSION" > "$ENV_FILE"
fi

FILES=(-f "$REPO_ROOT/docker-compose.yml")
if [ "$BETA" -eq 1 ]; then
    FILES+=(-f "$REPO_ROOT/docker-compose.beta.yml")
fi

echo "[deploy] APP_VERSION=$APP_VERSION (image tag djinn-netrunner-ops-web:$APP_VERSION)"
echo "[deploy] docker compose ${FILES[*]} up -d --build ${EXTRA[*]:-}"
exec docker compose "${FILES[@]}" up -d --build "${EXTRA[@]}"

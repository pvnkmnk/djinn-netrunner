#!/usr/bin/env bash
# NetRunner deploy.
#
# Brings the stack up with the image stamped by the version of the tag you are
# on, instead of whatever APP_VERSION happened to be exported in the shell:
#
#   ./scripts/deploy.sh                       # dev: base stack only
#   ./scripts/deploy.sh --release             # release overlay
#   ./scripts/deploy.sh --release --profile media-server
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
# replaced in place) so `scripts/smoke.sh` — which reads the declared
# version from there — checks against the same value the image was built with.
#
# Version resolution, first match wins — implemented in scripts/version.sh,
# which the published-image workflow calls with the ref it is building, so both
# release paths are stamped by one rule:
#   1. `APP_VERSION` in the environment — an explicit override, for building an
#      untagged commit (e.g. CI).
#   2. the tag pointing at HEAD (`git describe --tags --exact-match`).
#   3. `dev` — deliberately not a release number, so an untagged build reports
#      `NetRunner vdev` and can never masquerade as a release.
#
# Arguments:
#   --release         also apply docker-compose.release.yml (production overlay)
#   --profile <name>  enable a compose profile. `--profile` is a compose *global*
#                     option and is rejected after the subcommand, so the script
#                     positions it before `up`.
#   <anything else>   passed through to `docker compose up`
#
# Environment:
#   APP_VERSION  explicit override of the derived version

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

RELEASE=0
PROFILES=()
EXTRA=()
while [ $# -gt 0 ]; do
    case "$1" in
        --release) RELEASE=1 ;;
        --profile)
            [ $# -ge 2 ] || { echo "[deploy] --profile needs a value" >&2; exit 2; }
            PROFILES+=(--profile "$2"); shift ;;
        --profile=*) PROFILES+=("$1") ;;
        -h|--help) awk 'NR == 1 { next } /^set -euo pipefail/ { exit } { sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) EXTRA+=("$1") ;;
    esac
    shift
done

# Resolution and validation both live in scripts/version.sh — the one owner
# shared with the published-image workflow, so neither which version is stamped
# nor what may be one can drift between the two paths. It exits non-zero on a
# value that cannot tag an image, and `set -e` aborts this bring-up there rather
# than after a partial build.
APP_VERSION="$(bash "$SCRIPT_DIR/version.sh" "${APP_VERSION:-}")"
export APP_VERSION

# Persist it where `docker compose` (for ${APP_VERSION} substitution and as the
# image tag) and scripts/smoke.sh (for the declared-version check) both
# read it. Idempotent: an existing APP_VERSION line is replaced, not appended.
ENV_FILE="$REPO_ROOT/.env"
if [ -f "$ENV_FILE" ]; then
    ENV_TMP="$ENV_FILE.deploy.tmp"
    # cp -p before rewriting: .env holds secrets and is often mode 0600, and a
    # fresh file created by the shell's redirection would land on the umask's
    # default instead. Truncating the copy keeps the original mode and owner.
    cp -p "$ENV_FILE" "$ENV_TMP"
    # Replace the first APP_VERSION line and drop any duplicates: both compose
    # and smoke.sh resolve the *last* value, so a leftover later line would
    # silently win over the version this script reported and built.
    awk -v v="$APP_VERSION" '
        /^APP_VERSION=/ {
            if (!set) print "APP_VERSION=" v
            set = 1
            next
        }
        { print }
        END { if (!set) print "APP_VERSION=" v }
    ' "$ENV_FILE" > "$ENV_TMP"
    mv "$ENV_TMP" "$ENV_FILE"
else
    printf 'APP_VERSION=%s\n' "$APP_VERSION" > "$ENV_FILE"
fi

FILES=(-f "$REPO_ROOT/docker-compose.yml")
if [ "$RELEASE" -eq 1 ]; then
    FILES+=(-f "$REPO_ROOT/docker-compose.release.yml")
fi

echo "[deploy] APP_VERSION=$APP_VERSION (image tag djinn-netrunner-ops-web:$APP_VERSION)"
echo "[deploy] docker compose ${PROFILES[*]:-} ${FILES[*]} up -d --build ${EXTRA[*]:-}"
exec docker compose "${PROFILES[@]}" "${FILES[@]}" up -d --build "${EXTRA[@]}"

#!/usr/bin/env bash
# NetRunner version resolution — the one owner of the APP_VERSION rule.
#
# Both paths that stamp a version call this, so the value a build reports and the
# constraint on that value cannot drift apart:
#
#   scripts/deploy.sh               # local bring-up (compose + image tags)
#   .github/workflows/docker.yml    # published image (Dockerfile build arg)
#
# Usage:
#   scripts/version.sh [candidate]
#
# Resolution, first match wins:
#   1. the argument, or `APP_VERSION` in the environment — an explicit value,
#      which is how CI passes the ref it is building;
#   2. the tag pointing at HEAD (`git describe --tags --exact-match`);
#   3. `dev` — the deliberate sentinel, never a release number, so a build that
#      was never told its version reports `NetRunner vdev` instead of claiming a
#      numbered release it is not.
#
# The result is validated as a Docker image tag before it is printed. A git tag
# may legally contain a slash or a plus (`release/1.0.0`, `v1.0.0+build`), which
# would either make `image: djinn-netrunner-ops-web:$APP_VERSION` an invalid
# reference and fail the whole bring-up, or stamp a published image's footer with
# a version that is not a release. Rejected here, once, for both callers.
#
# Prints the version on stdout. On a value that cannot tag an image, prints the
# reason on stderr and exits non-zero, so a caller running under `set -e` fails
# before it builds anything.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="${1:-${APP_VERSION:-}}"

if [ -z "$VERSION" ]; then
    # `describe --exact-match` fails (non-zero) off a tag, the common case
    # during development, which is the signal to take the sentinel.
    if ! VERSION="$(git -C "$REPO_ROOT" describe --tags --exact-match HEAD 2>/dev/null)"; then
        VERSION="dev"
    fi
fi

# Docker image tags allow [A-Za-z0-9_][A-Za-z0-9_.-]{0,127}.
if [[ ! "$VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
    echo "[version] '$VERSION' is not a valid Docker tag (letters, digits, '_', '.', '-' only)" >&2
    echo "[version] a git tag like 'release/1.0.0' or 'v1.0.0+build' cannot tag an image; override with APP_VERSION=v1.0.0" >&2
    exit 1
fi

printf '%s\n' "$VERSION"

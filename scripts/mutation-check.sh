#!/bin/bash
# Mutation check runner (GA gap C9): proves a probe spec BITES by applying a
# targeted mutation to the behavior under test, expecting the spec to FAIL,
# then restoring and expecting it to PASS. A green spec that stays green with
# its subject removed is testing nothing.
#
# Usage: scripts/mutation-check.sh <mutation>
#   gate      - make the identity gate accept everything (download_gate.go);
#               the Soulseek wrong-work spec must fail.
#   boundary  - unset YTDLP_PROXY so yt-dlp egress is unvalidated;
#               the multi-hop post-handover spec must fail.
#
# Each mutation is applied in-place, the stack is rebuilt with it, the spec is
# expected to FAIL, then the file is restored and the stack rebuilt clean.
# Exit 0 = the spec bit (mutation caught); exit 1 = the spec passed WITH the
# mutation (it is testing nothing) or the harness broke.
#
# The restore uses `git stash`-free snapshot copies taken BEFORE mutating, not
# `git checkout --`: these files carry uncommitted work in progress, and a
# checkout would silently wipe it (observed live — the fake-slskd service
# block vanished from the overlay mid-run).set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MUTATION="${1:?usage: scripts/mutation-check.sh <gate|boundary>}"
SPEC="e2e/tests/ga-probes.spec.ts"
DOCKER="${DOCKER_BIN:-docker}"

if [ -n "${ProgramFiles:-}" ] && [ -x "${ProgramFiles}/Docker/Docker/resources/bin/docker.exe" ]; then
  DOCKER="${ProgramFiles}/Docker/Docker/resources/bin/docker.exe"
fi

compose() {
  ${DOCKER} compose --env-file .env.e2e -f docker-compose.yml -f docker-compose.e2e.yml "$@"
}

service_for() {
  case "$MUTATION" in
    gate)     echo "ops-worker" ;;
    boundary) echo "ops-worker" ;;
    *) echo "unknown mutation: $MUTATION (gate|boundary)" >&2; exit 2 ;;
  esac
}

apply_mutation() {
  case "$MUTATION" in
    # The gate's verdict is consumed at the mismatch branch in
    # download_gate.go: make it read as "no mismatch" and the file imports.
    gate)
      python - <<'PY'
import pathlib
p = pathlib.Path("backend/internal/services/download_gate.go")
t = p.read_text(encoding="utf-8")
anchor = 'mismatch := identityMismatch(&p.item, meta)'
assert t.count(anchor) == 1, f"anchor not unique: {anchor}"
t = t.replace(anchor, '_ = meta // MUTATION: identity verdict ignored\n\tmismatch := ""')
p.write_text(t, encoding="utf-8", newline="")
PY
      ;;
    # No proxy = no validating boundary in front of yt-dlp; the private hop
    # would be followed and only the (absent) pre-flight could refuse it.
    boundary)
      sed -i 's/^      YTDLP_PROXY: http:\/\/egress-proxy:3128$/      # MUTATION: YTDLP_PROXY removed/' docker-compose.e2e.yml
      ;;
  esac
}

restore_mutation() {
  case "$MUTATION" in
    gate)     mv backend/internal/services/download_gate.go.bak backend/internal/services/download_gate.go ;;
    boundary) mv docker-compose.e2e.yml.bak docker-compose.e2e.yml ;;
  esac
}

cleanup() {
  cd "$REPO_ROOT"
  if [ -f "backend/internal/services/download_gate.go.bak" ] || [ -f "docker-compose.e2e.yml.bak" ]; then
    restore_mutation
  fi
  echo "[mutation-check] restored $MUTATION; rebuilding clean stack..."
  compose up -d --build "$(service_for)" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[mutation-check] snapshotting the target file (it may carry uncommitted work)..."
case "$MUTATION" in
  gate)     cp backend/internal/services/download_gate.go backend/internal/services/download_gate.go.bak ;;
  boundary) cp docker-compose.e2e.yml docker-compose.e2e.yml.bak ;;
esac

echo "[mutation-check] applying mutation: $MUTATION"
apply_mutation

echo "[mutation-check] rebuilding $(service_for) with the mutation..."
compose up -d --build "$(service_for)" >/dev/null
sleep 10

echo "[mutation-check] running the spec — it MUST FAIL..."
cd "$REPO_ROOT/e2e"
# The pipeline's exit code is tail's, so run playwright with its own status
# captured (PIPESTATUS) — a masked failure would read as "spec does not bite".
set +e
npx playwright test "$(basename "$SPEC")" --timeout=300000 --reporter=list --workers=1 > /tmp/mutation-playwright.log 2>&1
SPEC_EXIT=$?
tail -6 /tmp/mutation-playwright.log
set -e
cd "$REPO_ROOT"
if [ "$SPEC_EXIT" -eq 0 ]; then
  echo "[mutation-check] SPEC PASSED WITH THE MUTATION — the spec does not bite."
  exit 1
fi
echo "[mutation-check] spec failed under the mutation (exit $SPEC_EXIT), as it must."

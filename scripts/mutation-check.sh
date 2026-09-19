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
# block vanished from the overlay mid-run).
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MUTATION="${1:?usage: scripts/mutation-check.sh <gate|boundary>}"
SPEC="e2e/tests/ga-probes.spec.ts"
DOCKER="${DOCKER_BIN:-docker}"

# Prefer the PATH-resolved docker; fall back to Docker Desktop's known
# install location. Either way, docker-credential-desktop (a SIBLING of
# docker.exe) is resolved by the compose/buildx client from PATH at build
# time — a missing helper kills every build with "docker-credential-desktop
# ... not found in %PATH%". Prepend the bin dir in POSIX form (cygpath -u):
# a Windows-form entry mangles the MSYS PATH list instead of extending it.
DOCKER="$(command -v docker || true)"
if [ -z "$DOCKER" ] && [ -n "${ProgramFiles:-}" ] && [ -x "${ProgramFiles}/Docker/Docker/resources/bin/docker.exe" ]; then
  DOCKER="${ProgramFiles}/Docker/Docker/resources/bin/docker.exe"
fi
[ -n "$DOCKER" ] || { echo "docker not found on PATH or at the Docker Desktop default" >&2; exit 2; }
DOCKER_BIN_DIR="$(dirname "$DOCKER")"
if command -v cygpath >/dev/null 2>&1; then
  DOCKER_BIN_DIR="$(cygpath -u "$DOCKER_BIN_DIR")"
fi
export PATH="$DOCKER_BIN_DIR:$PATH"

# Python 3 under different names: `python3` on Linux, `python` on
# Windows-as-Python-Launcher hosts. Validated by EXECUTION, not presence:
# Windows' Store `python3` alias exists on PATH but only prints "Python was
# not found" — and a silently skipped mutation must not read as a green run.
PY_BIN=""
for candidate in python3 python py; do
  if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c "import sys" >/dev/null 2>&1; then
    PY_BIN="$candidate"; break
  fi
done

# Fresh checkouts (CI runners) have no .env.e2e; compose refuses to start
# without it. Same bootstrap as scripts/e2e.sh's ensure_env_file — not
# sourced because e2e.sh dispatches on "$1" and would run the test suite.
if [ ! -f .env.e2e ]; then
  if [ ! -f .env.e2e.example ]; then
    echo ".env.e2e missing and no .env.e2e.example to copy" >&2; exit 2
  fi
  cp .env.e2e.example .env.e2e
  echo "[mutation-check] created .env.e2e from the checked-in template"
fi

compose() {
  "$DOCKER" compose --env-file .env.e2e -f docker-compose.yml -f docker-compose.e2e.yml "$@"
}

service_for() {
  case "$MUTATION" in
    gate)     echo "ops-worker" ;;
    boundary) echo "ops-worker" ;;
    *) echo "unknown mutation: $MUTATION (gate|boundary)" >&2; exit 2 ;;
  esac
}

# Only the test that pins the mutated behavior — running the whole file would
# blame an infrastructure blip in an unrelated probe on the mutation.
test_grep_for() {
  case "$MUTATION" in
    gate)     echo "Soulseek entrance" ;;
    boundary) echo "multi-hop" ;;
  esac
}

apply_mutation() {
  case "$MUTATION" in
    # The gate's verdict is consumed at the mismatch branch in
    # download_gate.go: make it read as "no mismatch" and the file imports.
    gate)
      if [ -z "$PY_BIN" ]; then
        echo "gate mutation needs python3/python/py on PATH" >&2; exit 2
      fi
      "$PY_BIN" - <<'PY'
import pathlib
p = pathlib.Path("backend/internal/services/download_gate.go")
t = p.read_text(encoding="utf-8")
anchor = 'mismatch := identityMismatch(&p.item, meta)'
assert t.count(anchor) == 1, f"anchor not unique: {anchor}"
t = t.replace(anchor, '_ = meta // MUTATION: identity verdict ignored\n\tmismatch := ""')
p.write_text(t, encoding="utf-8", newline="")
PY
      # A no-op mutation (stub interpreter, anchor drift) must fail here,
      # not masquerade as a green cycle.
      grep -q "MUTATION: identity verdict ignored" backend/internal/services/download_gate.go \
        || { echo "gate mutation did not land" >&2; exit 2; }
      ;;
    # No proxy = no validating boundary in front of yt-dlp; the private hop
    # would be followed and only the (absent) pre-flight could refuse it.
    boundary)
      sed -i 's/^      YTDLP_PROXY: http:\/\/egress-proxy:3128$/      # MUTATION: YTDLP_PROXY removed/' docker-compose.e2e.yml
      grep -q "MUTATION: YTDLP_PROXY removed" docker-compose.e2e.yml \
        || { echo "boundary mutation did not land" >&2; exit 2; }
      ;;
  esac
}

restore_mutation() {
  case "$MUTATION" in
    gate)     mv backend/internal/services/download_gate.go.bak backend/internal/services/download_gate.go ;;
    boundary) mv docker-compose.e2e.yml.bak docker-compose.e2e.yml ;;
  esac
}

# Restore + rebuild in one step, guarded by the .bak files so it is
# idempotent. Called explicitly BEFORE the control run (the control run must
# exercise the clean tree — the EXIT trap alone would fire too late, after the
# control run, and the control would fail with the mutation still applied).
cleanup() {
  cd "$REPO_ROOT"
  if [ -f "backend/internal/services/download_gate.go.bak" ] || [ -f "docker-compose.e2e.yml.bak" ]; then
    restore_mutation
    echo "[mutation-check] restored $MUTATION; rebuilding clean stack..."
    compose up -d --build "$(service_for)" >/dev/null 2>&1 || true
  fi
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

run_probe() {
  # Subshell: the cd must not outlive the call, or the script's cwd (and the
  # relative log paths and mutation file targets below) silently re-anchor to
  # e2e/ after the first probe run.
  ( cd "$REPO_ROOT/e2e" && npx playwright test "$(basename "$SPEC")" --grep "$(test_grep_for)" \
    --timeout=300000 --reporter=list --workers=1 )
}

echo "[mutation-check] running the probe — it MUST FAIL..."
# Distinguish "the spec caught the mutation" from "the stack broke": a failing
# SPEC means caught; anything else (compose failed, npx missing) means the
# harness itself is broken and must fail loudly, not "pass".
set +e
run_probe > "$REPO_ROOT/mutation-run.tmp.log" 2>&1
SPEC_EXIT=$?
set -e
tail -6 "$REPO_ROOT/mutation-run.tmp.log"
rm -f "$REPO_ROOT/mutation-run.tmp.log"
if [ "$SPEC_EXIT" -eq 0 ]; then
  echo "[mutation-check] SPEC PASSED WITH THE MUTATION — the spec does not bite."
  exit 1
fi
case "$SPEC_EXIT" in
  1) echo "[mutation-check] probe failed under the mutation (exit $SPEC_EXIT), as it must." ;;
  *) echo "[mutation-check] probe exited $SPEC_EXIT — a harness/infra failure, not a caught mutation." >&2; exit 3 ;;
esac

# Clean control run: restore FIRST (the EXIT trap fires too late to help
# here), rebuild, then run. The probe must PASS — otherwise the "failure"
# above was a flaky or broken probe, not a caught mutation, and the check has
# proven nothing.
echo "[mutation-check] restoring before the control run..."
cleanup
echo "[mutation-check] clean control run — the probe MUST PASS now..."
set +e
run_probe > "$REPO_ROOT/mutation-control.tmp.log" 2>&1
CONTROL_EXIT=$?
set -e
tail -4 "$REPO_ROOT/mutation-control.tmp.log"
rm -f "$REPO_ROOT/mutation-control.tmp.log"
if [ "$CONTROL_EXIT" -ne 0 ]; then
  echo "[mutation-check] CONTROL RUN FAILED — the probe's mutation failure cannot be trusted." >&2
  exit 4
fi
echo "[mutation-check] PASS: probe fails under the mutation, passes restored — it bites."

#!/usr/bin/env bash
# UI visual harness -- one entry point for the batched before/after capture the
# UI pass runs its verification on. See e2e/visual/README.md for the full story.
#
#   scripts/ui-visual.sh capture before        record every named surface
#   scripts/ui-visual.sh capture after         ... again after the change
#   scripts/ui-visual.sh compare before after  side-by-side + pixel diff report
#   scripts/ui-visual.sh mint-session          session for the seeded-content account
#   scripts/ui-visual.sh list                  the surface list
#
# Env:
#   UI_VIS_BASE_URL      running stack (default http://127.0.0.1:8080)
#   UI_VIS_SESSION       session_id cookie value (else .session, else login)
#   UI_VIS_EMAIL/PASSWORD  account to log in as when there is no session
#   UI_VIS_OUT           output root (default e2e/visual-output)
#   UI_VIS_DB_CONTAINER/USER/NAME  mint-session psql target
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_ROOT="${UI_VIS_OUT:-$REPO_ROOT/e2e/visual-output}"
BASE_URL="${UI_VIS_BASE_URL:-http://127.0.0.1:8080}"
DB_CONTAINER="${UI_VIS_DB_CONTAINER:-netrunner-postgres}"
DB_USER="${UI_VIS_DB_USER:-musicops}"
DB_NAME="${UI_VIS_DB_NAME:-musicops}"
# The account that owns the seeded library (117 tracks) in the dev database.
SEEDED_OWNER_EMAIL="${UI_VIS_OWNER_EMAIL:-task6.beta@smoke.test}"

usage() {
  sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

cmd_capture() {
  local label="${1:-}"
  [ -n "$label" ] && shift
  [ -n "$label" ] || { echo "capture needs a label: capture before|after|<name>" >&2; exit 2; }
  node "$REPO_ROOT/e2e/visual/capture.mjs" --label "$label" --base-url "$BASE_URL" --out "$OUT_ROOT" "$@"
}

cmd_compare() {
  local before="${1:-before}" after="${2:-after}"
  shift 2 2>/dev/null || true
  node "$REPO_ROOT/e2e/visual/compare.mjs" --before "$before" --after "$after" --out-root "$OUT_ROOT" "$@"
}

# A session_id cookie for an account that owns seeded content. The dev password
# is not known to the harness and resetting it would mutate seeded state; one
# expiring sessions row does not. Requires the stack's postgres container.
cmd_mint_session() {
  local user="${1:-$SEEDED_OWNER_EMAIL}" uid sid
  case "$user" in
    *"'"*) echo "email must not contain a quote" >&2; exit 2 ;;
  esac
  command -v docker >/dev/null || { echo "docker not on PATH" >&2; exit 2; }
  uid="$(docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT id FROM users WHERE email = '$user'" 2>/dev/null | tr -d '\r' | head -1)"
  [ -n "$uid" ] || { echo "no user '$user' in $DB_NAME on $DB_CONTAINER" >&2; exit 1; }
  sid="$(node -e 'console.log(require("node:crypto").randomUUID())')"
  docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -q -c \
    "INSERT INTO sessions (session_id, user_id, created_at, expires_at) VALUES ('$sid', $uid, now(), now() + interval '12 hours')" >/dev/null
  mkdir -p "$OUT_ROOT"
  printf '%s\n' "$sid" > "$OUT_ROOT/.session"
  chmod 600 "$OUT_ROOT/.session" 2>/dev/null || true
  echo "minted a 12h session for $user (user id $uid)"
  echo "stored in $OUT_ROOT/.session -- capture reads it automatically"
  echo "export UI_VIS_SESSION=$sid"
}

cmd_list() {
  node -e '
    const fs = require("node:fs");
    const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    for (const surface of manifest.surfaces) {
      console.log(`${surface.kind.padEnd(12)} ${surface.name.padEnd(22)} ${surface.path || surface.mount}`);
    }
  ' "$REPO_ROOT/e2e/visual/surfaces.json"
}

case "${1:-}" in
  capture) shift; cmd_capture "$@" ;;
  compare) shift; cmd_compare "$@" ;;
  mint-session) shift; cmd_mint_session "$@" ;;
  list) shift; cmd_list "$@" ;;
  ""|-h|--help|help) usage ;;
  *) echo "unknown command: $1" >&2; usage; exit 2 ;;
esac

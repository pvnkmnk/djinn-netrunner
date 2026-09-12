#!/usr/bin/env bash
# NetRunner beta smoke test
#
# Asserts that a *running* deployment is actually healthy — the things the
# deploy-and-tear-down scripts/smoke-test.sh cannot see: that configuration
# reached the containers, that sessions survive a restart, that scanning indexes
# every file it finds, and that a Subsonic client can stream the result.
#
# Usage:
#   ./scripts/beta-smoke.sh                                  # http://localhost:8080
#   BETA_BASE_URL=https://music.example ./scripts/beta-smoke.sh
#   BETA_COMPOSE_ARGS="-f docker-compose.yml -f docker-compose.beta.yml" ./scripts/beta-smoke.sh
#
# Options:
#   --keep    leave the smoke user, library and audio fixtures in place
#
# Exits non-zero if any check fails.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BASE_URL="${BETA_BASE_URL:-http://localhost:8080}"
COMPOSE_ARGS="${BETA_COMPOSE_ARGS:--f docker-compose.yml -f docker-compose.beta.yml}"
WEB_CONTAINER="${BETA_WEB_CONTAINER:-ops-web}"
WORKER_CONTAINER="${BETA_WORKER_CONTAINER:-ops-worker}"

SMOKE_USER="beta-smoke+$(date +%s)@smoke.test"
SMOKE_PASS="Betasmoke123!"
LIBRARY_PATH="/app/music/beta-smoke-$(date +%s)"
COOKIE_FILE="$(mktemp)"
BODY_FILE="$(mktemp)"
STATUS_FILE="$(mktemp)"
KEEP=0

for arg in "$@"; do
    case "$arg" in
        --keep) KEEP=1 ;;
        -h|--help) sed -n '2,19p' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) echo "unknown option: $arg" >&2; exit 2 ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FAILURES=0
pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${YELLOW}[INFO]${NC} $1"; }

compose() { docker compose $COMPOSE_ARGS "$@"; }
in_web() { compose exec -T "$WEB_CONTAINER" "$@"; }

cleanup() {
    if [ "$KEEP" -eq 0 ] && [ -n "${LIB_ID:-}" ]; then
        info "Removing smoke library and fixtures..."
        curl -s -X DELETE "$BASE_URL/api/libraries/$LIB_ID" \
            -H "X-CSRF-Token: $(csrf)" -b "$COOKIE_FILE" -c "$COOKIE_FILE" >/dev/null 2>&1 || true
        in_web sh -c "rm -rf '$LIBRARY_PATH'" >/dev/null 2>&1 || true
    elif [ "$KEEP" -eq 1 ]; then
        info "Left smoke library (${LIB_ID:-none}) and fixtures at $LIBRARY_PATH"
    fi
    rm -f "$COOKIE_FILE" "$BODY_FILE" "$STATUS_FILE"
    echo
    if [ "$FAILURES" -eq 0 ]; then
        echo -e "${GREEN}Beta smoke: all checks passed.${NC}"
    else
        echo -e "${RED}Beta smoke: $FAILURES check(s) failed.${NC}"
    fi
    exit "$FAILURES"
}
trap cleanup EXIT

csrf() { awk '$6 == "csrf_" {print $7}' "$COOKIE_FILE" | tail -1; }

# Make a request against the JSON API, refreshing the CSRF token first.
# The refresh must echo existing cookies back (-b and -c together) or it wipes
# the session cookie. Prints the response body; the HTTP code goes to
# STATUS_FILE, because a variable set inside $( ) never reaches the caller.
api() {
    local method="$1" path="$2" body="${3:-}"
    curl -s -b "$COOKIE_FILE" -c "$COOKIE_FILE" -o /dev/null "$BASE_URL/" >/dev/null 2>&1 || true
    local token; token="$(csrf)"
    local args=(-s -b "$COOKIE_FILE" -c "$COOKIE_FILE" -X "$method" "$BASE_URL$path"
                -H "X-CSRF-Token: $token" -w '%{http_code}' -o "$BODY_FILE")
    [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
    curl "${args[@]}" > "$STATUS_FILE"
    cat "$BODY_FILE"
}

status() { cat "$STATUS_FILE" 2>/dev/null || echo 000; }

# Authenticated GET for checks that must prove a real response. Prints the body
# and records the HTTP status in STATUS_FILE — for the same reason as api(), a
# variable assigned inside $( ) never reaches the caller, and checking only for
# an absence of "error" would accept an empty body from a failed request.
authed_get() {
    curl -s -b "$COOKIE_FILE" -w '%{http_code}' -o "$BODY_FILE" "$BASE_URL$1" > "$STATUS_FILE"
    cat "$BODY_FILE"
}

# A deployment still carrying a documented placeholder secret is not healthy:
# those values are published in the repo, so anyone can authenticate with them.
is_template_secret() {
    case "$1" in
        changeme|CHANGE_ME|smokepass|smoke-jwt-secret|smoke-api-key|your_random_api_key|generate_random_api_key|replace_with_a_long_random_secret|your_*) return 0 ;;
    esac
    return 1
}

json_field() { printf '%s' "$1" | grep -o "\"$2\":\"[^\"]*\"" | head -1 | sed 's/.*:"//;s/"$//'; }
# Counts occurrences of an already-quoted JSON fragment, e.g. '"title":"Smoke'.
json_count() { printf '%s' "$1" | grep -o -- "$2" | wc -l | tr -d ' '; }
urlencode() { printf '%s' "$1" | sed 's/@/%40/;s/+/%2B/'; }

echo "== NetRunner beta smoke =="
echo "base:    $BASE_URL"
echo "compose: docker compose $COMPOSE_ARGS"
echo

# ── 1. Prerequisites ────────────────────────────────────────────────────────
command -v docker >/dev/null || { fail "docker is not installed"; exit 1; }
command -v curl >/dev/null || { fail "curl is not installed"; exit 1; }
pass "docker and curl available"

# ── 2. Containers running and healthy ───────────────────────────────────────
for c in "$WEB_CONTAINER" "$WORKER_CONTAINER"; do
    state="$(docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null || echo missing)"
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$c" 2>/dev/null || echo none)"
    if [ "$state" = "running" ] && { [ "$health" = "healthy" ] || [ "$health" = "none" ]; }; then
        pass "$c is running (health: $health)"
    else
        fail "$c is not healthy (state=$state health=$health)"
    fi
done

# ── 3. ffmpeg present — tag writes shell out to it ──────────────────────────
if in_web sh -c 'command -v ffmpeg >/dev/null' >/dev/null 2>&1; then
    pass "ffmpeg present in $WEB_CONTAINER (required for M4A/OGG tag writes)"
else
    fail "ffmpeg missing from $WEB_CONTAINER — tag writes and transcoding will fail"
fi

# ── 4. Configuration actually reached the containers ────────────────────────
# .env is only meaningful if the services declare env_file:; a silent miss shows
# up as a per-restart random JWT secret and a dead Subsonic API.
for c in "$WEB_CONTAINER" "$WORKER_CONTAINER"; do
    env_dump="$(docker exec "$c" env 2>/dev/null || true)"
    for required in JWT_SECRET SUBSONIC_PASSWORD; do
        value="$(printf '%s' "$env_dump" | sed -n "s/^${required}=//p")"
        if [ -z "$value" ]; then
            fail "$c is missing $required — check env_file: in the compose service"
        elif is_template_secret "$value"; then
            fail "$c is still using the template value for $required — generate a real one (openssl rand -base64 48)"
        else
            pass "$c has a non-template $required set"
        fi
    done
    if printf '%s' "$env_dump" | grep -q "^SUBSONIC_ENABLED=true"; then
        pass "$c has Subsonic enabled"
    else
        fail "$c does not have SUBSONIC_ENABLED=true — the streaming API will 404"
    fi
done

# ── 5. Health endpoint ──────────────────────────────────────────────────────
if curl -sf "$BASE_URL/api/health" >/dev/null 2>&1; then
    pass "/api/health responded"
else
    fail "/api/health did not respond — is the deployment up at $BASE_URL?"
fi

# ── 6. Register + login ─────────────────────────────────────────────────────
REGISTER="$(api POST /api/auth/register "{\"email\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASS\"}")"
if [ "$(status)" = "201" ] || printf '%s' "$REGISTER" | grep -q '"status":"ok"'; then
    pass "registered $SMOKE_USER"
else
    fail "registration failed (status $(status)): $REGISTER"
fi

# Login is an HTMX-first endpoint: it answers 302 + Set-Cookie, not JSON.
LOGIN="$(api POST /api/auth/login "{\"email\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASS\"}")"
if grep -q 'session_id' "$COOKIE_FILE"; then
    pass "logged in (session cookie issued, status $(status))"
else
    fail "login issued no session cookie (status $(status)): $LOGIN"
fi

# ── 7. Session survives a restart ───────────────────────────────────────────
# The original failure mode: JWT_SECRET never reached the process, so every
# restart minted a new random secret and silently invalidated all sessions.
info "Restarting $WEB_CONTAINER to check session persistence..."
compose restart "$WEB_CONTAINER" >/dev/null 2>&1
for _ in $(seq 1 30); do
    curl -sf "$BASE_URL/api/health" >/dev/null 2>&1 && break
    sleep 2
done

libs_after="$(authed_get /api/libraries)"
if [ "$(status)" != "200" ]; then
    fail "session did not survive the restart (status $(status)): $libs_after"
elif printf '%s' "$libs_after" | grep -qi '"error"'; then
    fail "session did not survive the restart: $libs_after"
else
    pass "session survived a $WEB_CONTAINER restart"
fi

# ── 8. Subsonic ping, both auth styles ──────────────────────────────────────
SUBSONIC_PASS="$(docker exec "$WEB_CONTAINER" printenv SUBSONIC_PASSWORD 2>/dev/null || true)"
QA="v=1.16.1&c=beta-smoke&f=json"
SU_U="u=$(urlencode "$SMOKE_USER")"

PING_ACCOUNT="$(curl -s "$BASE_URL/rest/ping.view?$QA&$SU_U&p=$SMOKE_PASS")"
if printf '%s' "$PING_ACCOUNT" | grep -q '"status":"ok"'; then
    pass "Subsonic ping accepted the account password"
else
    fail "Subsonic ping (account password) failed: $PING_ACCOUNT"
fi

if [ -n "$SUBSONIC_PASS" ]; then
    # The server compares against md5(md5(password_as_hex) + salt) — Subsonic's
    # documented token scheme, so the password is hashed before it is salted.
    SALT="smoke$(date +%s)"
    PASS_HASH="$(printf '%s' "$SUBSONIC_PASS" | md5sum | cut -d' ' -f1)"
    TOKEN="$(printf '%s' "${PASS_HASH}${SALT}" | md5sum | cut -d' ' -f1)"
    PING_TOKEN="$(curl -s "$BASE_URL/rest/ping.view?$QA&$SU_U&t=$TOKEN&s=$SALT")"
    if printf '%s' "$PING_TOKEN" | grep -q '"status":"ok"'; then
        pass "Subsonic ping accepted token auth"
    else
        fail "Subsonic ping (token) failed: $PING_TOKEN"
    fi
fi

# ── 9. Owned library + audio fixtures + scan ────────────────────────────────
in_web sh -c "mkdir -p '$LIBRARY_PATH/Every Time I Die/Gutter Phenomenon'"
FIXTURE_COUNT=3
for i in 1 2 3; do
    in_web sh -c "ffmpeg -y -hide_banner -loglevel error -f lavfi -i 'sine=frequency=44$i:duration=1' \
        -metadata 'title=Smoke Track $i' -metadata 'artist=Every Time I Die' \
        -metadata 'album=Gutter Phenomenon' '$LIBRARY_PATH/Every Time I Die/Gutter Phenomenon/0$i - Smoke $i.flac'" \
        >/dev/null 2>&1 || fail "could not generate test audio fixture $i"
done
pass "created $FIXTURE_COUNT audio fixtures under $LIBRARY_PATH"

LIBRARY_RESP="$(api POST /api/libraries "{\"name\":\"beta-smoke\",\"path\":\"$LIBRARY_PATH\"}")"
LIB_ID="$(json_field "$LIBRARY_RESP" ID)"
[ -n "$LIB_ID" ] || LIB_ID="$(json_field "$LIBRARY_RESP" id)"
if [ -n "$LIB_ID" ]; then
    pass "created library $LIB_ID owned by the smoke user"
else
    fail "library creation failed (status $(status)): $LIBRARY_RESP"
fi

SCAN="$(api POST "/api/libraries/$LIB_ID/scan")"
info "scan trigger -> status $(status)"

INDEXED=0
TRACKS_JSON=""
for _ in $(seq 1 30); do
    TRACKS_JSON="$(authed_get "/api/libraries/$LIB_ID/tracks")"
    [ "$(status)" = "200" ] || continue
    INDEXED="$(json_count "$TRACKS_JSON" "\"title\":\"Smoke Track")"
    [ "$INDEXED" -ge "$FIXTURE_COUNT" ] && break
    sleep 2
done

if [ "$INDEXED" -eq "$FIXTURE_COUNT" ]; then
    pass "scan indexed all $FIXTURE_COUNT fixtures"
else
    fail "scan indexed $INDEXED of $FIXTURE_COUNT fixtures — check server logs for per-file scan errors"
fi

# Every indexed track must carry its own path. Rows inserted without a path all
# collide on the unique idx_tracks_path index, which is how a scan of N files
# ends up reporting success while storing exactly one row. Match on the path
# prefix specifically: each track also nests a zero-valued "library" object
# whose own (unloaded) "path" is empty, so counting '"path":""' would
# false-positive on that instead.
PATHFUL="$(json_count "$TRACKS_JSON" "\"path\":\"$LIBRARY_PATH")"
if [ "$PATHFUL" -eq "$FIXTURE_COUNT" ]; then
    pass "every indexed track has a persisted path under the library root"
else
    fail "$PATHFUL of $FIXTURE_COUNT indexed tracks have a persisted path"
fi

# ── 10. Subsonic sees the library it owns ───────────────────────────────────
SU="$SU_U&p=$SMOKE_PASS"
INDEXES="$(curl -s "$BASE_URL/rest/getIndexes.view?$QA&$SU")"
if printf '%s' "$INDEXES" | grep -q 'Every Time I Die'; then
    pass "Subsonic getIndexes lists the library artist"
else
    fail "Subsonic getIndexes does not list the library artist: $INDEXES"
fi

SEARCH="$(curl -s "$BASE_URL/rest/search3.view?$QA&$SU&query=Smoke")"
TRACK_ID="$(json_field "$SEARCH" id)"
if [ -n "$TRACK_ID" ]; then
    pass "Subsonic search3 returned track $TRACK_ID"
else
    fail "Subsonic search3 returned no tracks: $SEARCH"
fi

# ── 11. Streaming returns real audio ────────────────────────────────────────
if [ -n "$TRACK_ID" ]; then
    STREAM_BYTES="$(curl -s "$BASE_URL/rest/stream.view?$QA&$SU&id=$TRACK_ID" | wc -c | tr -d ' ')"
    if [ "$STREAM_BYTES" -gt 1000 ]; then
        pass "stream.view returned $STREAM_BYTES bytes of audio"
    else
        fail "stream.view returned $STREAM_BYTES bytes — expected real audio"
    fi
else
    fail "skipping stream check: no track id from search3"
fi

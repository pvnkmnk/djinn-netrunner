#!/usr/bin/env bash
# NetRunner Smoke Test
# Deploys Docker Compose stack, runs basic health/auth/CRUD checks, then tears down.
# Usage: ./scripts/smoke-test.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/docker-compose.yml"
PROJECT_NAME="${SMOKE_PROJECT_NAME:-netrunner-smoke}"
SMOKE_HTTP_PORT="${SMOKE_HTTP_PORT:-18081}"
SMOKE_HTTPS_PORT="${SMOKE_HTTPS_PORT:-18443}"
SMOKE_HTTPS_UDP_PORT="${SMOKE_HTTPS_UDP_PORT:-18443}"
BASE_URL="http://localhost:${SMOKE_HTTP_PORT}"
COOKIE_FILE="$(mktemp)"
OVERRIDE_FILE="$(mktemp)"

export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-smokepass}"
export SLSKD_USERNAME="${SLSKD_USERNAME:-smokeuser}"
export SLSKD_PASSWORD="${SLSKD_PASSWORD:-smokepass}"
export SLSKD_API_KEY="${SLSKD_API_KEY:-smoke-api-key}"
export JWT_SECRET="${JWT_SECRET:-smoke-jwt-secret}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}[INFO]${NC} $1"; }

compose() {
    docker compose -f "$COMPOSE_FILE" -f "$OVERRIDE_FILE" -p "$PROJECT_NAME" "$@"
}

csrf_token() {
    awk '$6 == "csrf_" {print $7}' "$COOKIE_FILE" | tail -1
}

csrf_header() {
    token="$(csrf_token)"
    [ -n "$token" ] || fail "CSRF token was not issued"
    printf '%s' "$token"
}

# --- Dependencies ---
command -v docker &>/dev/null || fail "Docker not installed"
command -v curl &>/dev/null || fail "curl not installed"

# --- Cleanup handler ---
cleanup() {
    info "Tearing down stack..."
    compose down -v 2>/dev/null || true
    rm -f "$COOKIE_FILE" "$OVERRIDE_FILE"
}
trap cleanup EXIT

cat > "$OVERRIDE_FILE" <<YAML
services:
  postgres:
    container_name: ${PROJECT_NAME}-postgres
  slskd:
    container_name: ${PROJECT_NAME}-slskd
  gonic:
    container_name: ${PROJECT_NAME}-gonic
  ops-web:
    container_name: ${PROJECT_NAME}-ops-web
    ports:
      - "${SMOKE_HTTP_PORT}:8080"
    environment:
      SLSKD_URL: http://slskd:5030
      GONIC_URL: http://gonic:4747
      JWT_SECRET: ${JWT_SECRET}
  ops-worker:
    container_name: ${PROJECT_NAME}-ops-worker
    environment:
      SLSKD_URL: http://slskd:5030
      GONIC_URL: http://gonic:4747
      JWT_SECRET: ${JWT_SECRET}
YAML

# --- 1. Deploy ---
info "Starting NetRunner stack..."
compose up -d --build postgres slskd gonic ops-web ops-worker 2>&1

# --- 2. Wait for health ---
info "Waiting for health endpoint..."
ATTEMPTS=0
MAX_ATTEMPTS=60
HEALTH_URL="$BASE_URL/api/health"
while [ $ATTEMPTS -lt $MAX_ATTEMPTS ]; do
    if curl -sf "$HEALTH_URL" > /dev/null 2>&1; then
        pass "Health endpoint responded"
        break
    fi
    ATTEMPTS=$((ATTEMPTS + 1))
    sleep 2
done
if [ $ATTEMPTS -ge $MAX_ATTEMPTS ]; then
    fail "Health endpoint not ready after 2 minutes"
fi

# --- 3. Register ---
info "Registering admin user..."
curl -sf -c "$COOKIE_FILE" "$BASE_URL/" > /dev/null 2>&1 || fail "Unable to fetch initial CSRF token"
SMOKE_EMAIL="admin+$(date +%s)@smoke.test"
CSRF_TOKEN="$(csrf_header)"
REGISTER_RESP=$(curl -sf -X POST "$BASE_URL/api/auth/register" \
    -H "Content-Type: application/json" \
    -H "X-CSRF-Token: $CSRF_TOKEN" \
    -b "$COOKIE_FILE" \
    -c "$COOKIE_FILE" \
    -d "{\"email\":\"$SMOKE_EMAIL\",\"password\":\"smoketest123\"}" 2>&1) || fail "Register failed: $REGISTER_RESP"
pass "Registration succeeded"

# --- 4. Login ---
info "Logging in..."
CSRF_TOKEN="$(csrf_header)"
LOGIN_RESP=$(curl -sf -X POST "$BASE_URL/api/auth/login" \
    -H "Content-Type: application/json" \
    -H "X-CSRF-Token: $CSRF_TOKEN" \
    -b "$COOKIE_FILE" \
    -c "$COOKIE_FILE" \
    -d "{\"email\":\"$SMOKE_EMAIL\",\"password\":\"smoketest123\"}" 2>&1) || fail "Login failed: $LOGIN_RESP"
pass "Login succeeded"

# --- 5. Create library ---
info "Creating test library..."
CSRF_TOKEN="$(csrf_header)"
LIBRARY_RESP=$(curl -sf -X POST "$BASE_URL/api/libraries" \
    -H "Content-Type: application/json" \
    -H "X-CSRF-Token: $CSRF_TOKEN" \
    -b "$COOKIE_FILE" \
    -c "$COOKIE_FILE" \
    -d '{"name":"smoke-test-lib","path":"/app/music"}' 2>&1) || fail "Create library failed: $LIBRARY_RESP"
pass "Library created"

# --- 6. List libraries ---
info "Listing libraries..."
LIBS=$(curl -sf "$BASE_URL/api/libraries" -b "$COOKIE_FILE" 2>&1) || fail "List libraries failed: $LIBS"
echo "$LIBS" | grep -q "smoke-test-lib" || fail "Library not found in list"
pass "Library listed"

# --- 7. Delete library ---
info "Cleaning up test library..."
LIB_ID=$(printf '%s' "$LIBS" | sed -n 's/.*"ID":"\([^"]*\)".*/\1/p' | head -1)
if [ -n "$LIB_ID" ]; then
    CSRF_TOKEN="$(csrf_header)"
    curl -sf -X DELETE "$BASE_URL/api/libraries/$LIB_ID" \
        -H "X-CSRF-Token: $CSRF_TOKEN" \
        -b "$COOKIE_FILE" \
        -c "$COOKIE_FILE" > /dev/null 2>&1 || true
fi
pass "Library cleaned up"

# --- Done ---
pass "All smoke tests passed!"

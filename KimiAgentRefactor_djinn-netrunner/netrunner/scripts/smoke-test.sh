#!/usr/bin/env bash
# NetRunner Smoke Test
# Deploys Docker Compose stack, runs basic health/auth/CRUD checks, then tears down.
# Usage: ./scripts/smoke-test.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/docker-compose.yml"
PROJECT_NAME="netrunner-smoke"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}[INFO]${NC} $1"; }

# --- Dependencies ---
command -v docker &>/dev/null || fail "Docker not installed"
command -v curl &>/dev/null || fail "curl not installed"

# --- Cleanup handler ---
cleanup() {
    info "Tearing down stack..."
    docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" down -v 2>/dev/null || true
}
trap cleanup EXIT

# --- 1. Deploy ---
info "Starting NetRunner stack..."
docker compose -f "$COMPOSE_FILE" -p "$PROJECT_NAME" up -d --build 2>&1

# --- 2. Wait for health ---
info "Waiting for health endpoint..."
ATTEMPTS=0
MAX_ATTEMPTS=60
HEALTH_URL="http://localhost:8080/api/health"
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
REGISTER_RESP=$(curl -sf -X POST http://localhost:8080/api/auth/register \
    -H "Content-Type: application/json" \
    -d '{"email":"admin@smoke.test","password":"smoketest123"}' 2>&1) || fail "Register failed: $REGISTER_RESP"
pass "Registration succeeded"

# --- 4. Login ---
info "Logging in..."
LOGIN_RESP=$(curl -sf -X POST http://localhost:8080/api/auth/login \
    -H "Content-Type: application/json" \
    -c "$REPO_ROOT/.smoke-cookies" \
    -d '{"email":"admin@smoke.test","password":"smoketest123"}' 2>&1) || fail "Login failed: $LOGIN_RESP"
pass "Login succeeded"

COOKIE_FILE="$REPO_ROOT/.smoke-cookies"

# --- 5. Create library ---
info "Creating test library..."
LIBRARY_RESP=$(curl -sf -X POST http://localhost:8080/api/libraries \
    -H "Content-Type: application/json" \
    -b "$COOKIE_FILE" \
    -d '{"name":"smoke-test-lib","path":"/tmp/smoke-music"}' 2>&1) || fail "Create library failed: $LIBRARY_RESP"
pass "Library created"

# --- 6. List libraries ---
info "Listing libraries..."
LIBS=$(curl -sf http://localhost:8080/api/libraries -b "$COOKIE_FILE" 2>&1) || fail "List libraries failed: $LIBS"
echo "$LIBS" | grep -q "smoke-test-lib" || fail "Library not found in list"
pass "Library listed"

# --- 7. Delete library ---
info "Cleaning up test library..."
LIB_ID=$(echo "$LIBS" | grep -oP '"ID":"[^"]+' | head -1 | cut -d'"' -f3)
if [ -n "$LIB_ID" ]; then
    curl -sf -X DELETE "http://localhost:8080/api/libraries/$LIB_ID" -b "$COOKIE_FILE" > /dev/null 2>&1 || true
fi
pass "Library cleaned up"

# --- Done ---
pass "All smoke tests passed!"

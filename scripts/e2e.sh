#!/bin/bash
# One-command Playwright E2E runner for NetRunner.
#
# Playwright owns the stack lifecycle here, not this script: its `webServer`
# config runs e2e/setup-test-db.sh (which builds the images, drops and recreates
# `musicops_test`, starts the services and seeds the admin user), and its
# `globalTeardown` brings the stack down with `-v` when CI=true.
#
# That matters because the previous CI workflow duplicated the bring-up with its
# own `docker compose up`, passing `--env-file .env.e2e` — a file that is not in
# the repo and had no template, so the job failed before Playwright ever ran and
# the suite recorded zero runs. This script closes that gap from the other side:
# it materialises `.env.e2e` from the checked-in `.env.e2e.example` when missing.
#
# Usage: scripts/e2e.sh [command]
#
# Commands:
#   test     Run the Playwright suite (default). Stack comes up and down around it.
#   up       Start the e2e stack only, for debugging a spec by hand.
#   down     Tear the e2e stack down and delete its volumes.
#   report   Open the last HTML report.
#   help     Show this help.
#
# Environment:
#   CI=true  Enables Playwright retries and makes teardown run. Set in CI.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
E2E_DIR="$REPO_ROOT/e2e"
ENV_FILE="$REPO_ROOT/.env.e2e"
ENV_TEMPLATE="$REPO_ROOT/.env.e2e.example"

# The same compose identity Playwright's setup/teardown scripts use. Kept in one
# place so `up`/`down` address the stack `test` actually built.
COMPOSE=(docker compose --env-file "$ENV_FILE" -f "$REPO_ROOT/docker-compose.yml" -f "$REPO_ROOT/docker-compose.e2e.yml")

show_help() {
    sed -n '2,26p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

die() {
    echo -e "${RED}Error: $1${NC}" >&2
    exit 1
}

check_deps() {
    command -v docker >/dev/null 2>&1 || die "docker is not installed"
    docker compose version >/dev/null 2>&1 || die "docker compose is not available"
    command -v npm >/dev/null 2>&1 || die "npm is not installed (needed for Playwright)"
    command -v curl >/dev/null 2>&1 || die "curl is not installed"
}

# Materialise .env.e2e from the template. Never overwrite an existing file — an
# operator may be pointing the suite at a deliberately different stack.
ensure_env_file() {
    if [ -f "$ENV_FILE" ]; then
        echo -e "${GREEN}Using existing .env.e2e${NC}"
        return
    fi
    [ -f "$ENV_TEMPLATE" ] || die ".env.e2e is missing and $ENV_TEMPLATE was not found"
    cp "$ENV_TEMPLATE" "$ENV_FILE"
    echo -e "${YELLOW}Created .env.e2e from .env.e2e.example${NC}"
}

install_playwright() {
    cd "$E2E_DIR"
    if [ ! -d node_modules ]; then
        echo -e "${YELLOW}Installing Playwright dependencies...${NC}"
        npm ci
    fi
    echo -e "${YELLOW}Ensuring chromium is installed...${NC}"
    # --with-deps needs root on Linux; CI runners have it, and a local user who
    # already has the browser pays only a version check.
    npx playwright install --with-deps chromium
}

run_tests() {
    ensure_env_file
    check_deps
    install_playwright
    cd "$E2E_DIR"
    echo -e "${YELLOW}Running Playwright suite...${NC}"
    npx playwright test
}

start_stack() {
    ensure_env_file
    check_deps
    echo -e "${YELLOW}Starting e2e stack (no tests)...${NC}"
    # setup-test-db.sh addresses compose with paths relative to e2e/, so it has to
    # run from there (Playwright's webServer already does; this command did not,
    # which made 'scripts/e2e.sh up' fail with "couldn't find env file").
    cd "$E2E_DIR"
    ./setup-test-db.sh
    echo -e "${GREEN}Stack is up on http://localhost:8080${NC}"
}

stop_stack() {
    check_deps
    [ -f "$ENV_FILE" ] || { echo "Nothing to stop (.env.e2e absent)"; return; }
    echo -e "${YELLOW}Tearing down e2e stack...${NC}"
    # Build the images were built from may differ; down -v always works.
    "${COMPOSE[@]}" down -v --remove-orphans
    echo -e "${GREEN}Torn down${NC}"
}

show_report() {
    [ -d "$E2E_DIR/playwright-report" ] || die "no report yet — run 'scripts/e2e.sh test' first"
    cd "$E2E_DIR"
    npx playwright show-report
}

case "${1:-test}" in
    test)   run_tests ;;
    up)     start_stack ;;
    down)   stop_stack ;;
    report) show_report ;;
    help|-h|--help) show_help ;;
    *)      echo -e "${RED}Unknown command: $1${NC}" >&2; show_help; exit 1 ;;
esac

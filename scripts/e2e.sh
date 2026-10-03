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

# The suite runs `workers: 1` / `fullyParallel: false` (e2e/playwright.config.ts),
# so ONE worker and ONE browser carry every test for the whole run. DJI-595
# recorded that worker fast-failing mid-run (0xC0000409) abandoned 176 tests and
# still left Playwright reporting a plausible partial count -- a number that
# looks like a result and is not one.
#
# Two changes address that, and only together:
#
#   1. Shard across N separate processes. Each shard gets a fresh worker and a
#      fresh browser, so lifetime is bounded by shard size rather than suite
#      size, and a death costs one shard instead of the rest of the run.
#   2. Prove the run was complete. `--list` gives the expected test count up
#      front; the shard counts must then sum to exactly that. A worker that
#      dies therefore FAILS the run instead of quietly shrinking the number.
#
# Shards run SEQUENTIALLY, deliberately. Raising `workers` would run tests
# concurrently against one shared database, which these fixtures are not built
# for (shared rate limiters, owner-scoped rows, per-scope job locks).
#
# E2E_SHARDS=1 restores the old single-process behaviour for debugging.
E2E_SHARDS="${E2E_SHARDS:-4}"

# Sum one category ("passed", "failed", "skipped", "flaky") across a shard log.
count_category() {
    sed -n "s/^ *\([0-9][0-9]*\) $2\b.*/\1/p" "$1" | awk '{s += $1} END {print s + 0}'
}

# How many tests the suite declares. This is the number the gate checks the
# run against, so it must come from Playwright rather than a constant -- a
# hard-coded expected count is the same brittleness DJI-592 just removed.
expected_test_count() {
    npx playwright test --list 2>/dev/null \
        | sed -n 's/^Total: \([0-9][0-9]*\) tests.*/\1/p' | tail -1
}

# The acceptance rule itself lives in scripts/e2e_gate.sh, which
# scripts/test_e2e_gate.sh exercises directly -- including a simulated worker
# death. A gate that can only be reached by a passing run is indistinguishable
# from a gate that never fires.
. "$SCRIPT_DIR/e2e_gate.sh"

run_tests() {
    ensure_env_file
    check_deps
    install_playwright
    cd "$E2E_DIR"

    local expected
    expected="$(expected_test_count)"
    [ -n "$expected" ] || die "could not read the expected test count from 'playwright test --list'"
    echo -e "${YELLOW}Suite declares ${expected} tests; running in ${E2E_SHARDS} shard(s).${NC}"

    local logs=() i log code
    local total_passed=0 total_failed=0 total_skipped=0 total_flaky=0

    for ((i = 1; i <= E2E_SHARDS; i++)); do
        # NOT under test-results/ or playwright-report/: Playwright clears its
        # own output directory at the start of every run, which unlinks a log
        # the shell still has open -- the first version of this died exactly
        # that way, with `sed: can't read shard-1.log` under `set -e`.
        log="$E2E_DIR/shard-logs/shard-$i.log"
        mkdir -p "$(dirname "$log")"
        echo -e "${YELLOW}Shard $i/${E2E_SHARDS}...${NC}"
        # A per-shard HTML output dir, because every shard would otherwise
        # overwrite the single playwright-report/ the CI workflow uploads.
        set +e
        PLAYWRIGHT_HTML_OUTPUT_DIR="playwright-report/shard-$i" \
            npx playwright test --shard="$i/$E2E_SHARDS" --reporter=list,html > "$log" 2>&1
        code=$?
        set -e
        logs+=("$log")

        total_passed=$(( total_passed + $(count_category "$log" passed) ))
        total_failed=$(( total_failed + $(count_category "$log" failed) ))
        total_skipped=$(( total_skipped + $(count_category "$log" skipped) ))
        total_flaky=$(( total_flaky + $(count_category "$log" flaky) ))

        if grep -aq 'did not run' "$log"; then
            echo -e "${RED}Shard $i abandoned tests (a worker died before running them).${NC}"
            sed -n '1,40p' "$log" | grep -aE 'worker process exited|Error' | sed 's/^/    /'
            tail -5 "$log" | sed 's/^/    /'
        fi
        if [ "$code" -ne 0 ]; then
            echo -e "${RED}Shard $i exited ${code}; tail follows:${NC}"
            tail -20 "$log" | sed 's/^/    /'
        fi
    done

    local executed=$(( total_passed + total_failed + total_skipped + total_flaky ))
    echo
    echo -e "${YELLOW}=== Suite totals across ${E2E_SHARDS} shard(s) ===${NC}"
    echo "  passed   : ${total_passed}"
    echo "  failed   : ${total_failed}"
    echo "  skipped  : ${total_skipped}"
    echo "  flaky    : ${total_flaky}"
    echo "  executed : ${executed} of ${expected} declared"

    # The gate. A run that lost a worker reports fewer tests than the suite
    # declares; that is a FAILED run, not a smaller number to read carefully.
    local reason
    if ! reason="$(verify_suite_complete "$expected" "${logs[@]}" 2>&1)"; then
        die "suite is not provably complete: ${reason}. See e2e/shard-logs/shard-*.log"
    fi
    echo -e "${GREEN}All ${executed} declared tests reported a result; suite is complete.${NC}"
    # Name the skips rather than just counting them, so a green run states
    # which coverage it is NOT providing and why.
    echo -e "${YELLOW}=== Accounted skips (${total_skipped}) ===${NC}"
    skipped_titles "${logs[@]}" | sort -u | while read -r t; do
        echo "  - ${t}"
    done
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

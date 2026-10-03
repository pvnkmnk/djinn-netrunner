#!/bin/bash
# Tests for the sharded-suite completeness gate (scripts/e2e_gate.sh).
#
# The point of these is the NEGATIVE controls. A gate proven only by a passing
# run proves nothing: it could be counting nothing, comparing nothing, or never
# reached. Each rejection case below feeds the real gate a crafted log
# representing the exact failure DJI-595 recorded.

set -uo pipefail

GATE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/e2e_gate.sh"
# shellcheck source=scripts/e2e_gate.sh
. "$GATE"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0

check() {  # check <description> <expect-fail:0|1> <expected> <shard logs...>
    local desc="$1" expect_fail="$2" expected="$3"; shift 3
    local out rc
    out="$(verify_suite_complete "$expected" "$@" 2>&1)"; rc=$?
    if [ "$expect_fail" = "0" ] && [ "$rc" -eq 0 ]; then
        PASS=$(( PASS + 1 )); echo "  ok    $desc"
    elif [ "$expect_fail" = "1" ] && [ "$rc" -ne 0 ]; then
        PASS=$(( PASS + 1 )); echo "  ok    $desc  (refused: ${out})"
    else
        FAIL=$(( FAIL + 1 )); echo "  FAIL  $desc  (rc=$rc, wanted failure=$expect_fail, said: ${out})"
    fi
}

# --- a realistic shard log: Playwright's list reporter summary -------------
good_a="$TMP/a.log"
cat > "$good_a" <<'EOF'
Running 73 tests using 1 worker

  ok  1 [chromium] > tests/jobs.spec.ts:49:7 > Jobs Feature (DJI-431) > 1. Page loads (1.5s)

  1 skipped
  72 passed (1.2m)
EOF

good_b="$TMP/b.log"
cat > "$good_b" <<'EOF'
Running 73 tests using 1 worker

  ok  1 [chromium] > tests/auth.spec.ts:22:9 > Auth & Navigation > Login Form (486ms)

  1 skipped
  72 passed (1.1m)
EOF

# --- the DJI-595 shape: a worker dies, the rest are abandoned --------------
dead="$TMP/dead.log"
cat > "$dead" <<'EOF'
Running 73 tests using 1 worker

  ok  1 [chromium] > tests/egress-refusal.spec.ts:40:3 > refusal probe (6.5m)

worker process exited unexpectedly (code=3221225794, signal=null)

  20 failed
  24 passed (8.1m)
  29 did not run
EOF

# --- silent truncation: no crash banner, just fewer tests than declared ----
truncated="$TMP/truncated.log"
cat > "$truncated" <<'EOF'
Running 73 tests using 1 worker

  ok  1 [chromium] > tests/watchlists.spec.ts:10:7 > Watchlists (1.0s)

  10 passed (0.4m)
EOF

echo "gate acceptance"
check "two complete shards summing to the declared total pass" 0 146 "$good_a" "$good_b"

echo
echo "gate rejection (these must NOT pass)"
check "a dead worker's 'did not run' is refused"          1 146 "$dead"
check "silently fewer tests than declared is refused"     1 146 "$truncated"
check "an incomplete pair is refused"                    1 146 "$good_a" "$truncated"
check "a missing shard log is refused"                   1 146 "$good_a" "$TMP/does-not-exist.log"

failing="$TMP/failing.log"
cat > "$failing" <<'EOF'
Running 73 tests using 1 worker

  2 failed
  70 passed (1.2m)
EOF
check "a failed test is refused"                         1 72 "$failing"

echo
echo "counting"
got="$(sum_shards "$good_a" "$good_b")"
if [ "$got" = "144 0 2 0" ]; then
    PASS=$(( PASS + 1 )); echo "  ok    sum_shards totals passed/failed/skipped/flaky = 144 0 2 0"
else
    FAIL=$(( FAIL + 1 )); echo "  FAIL  sum_shards returned '$got', wanted '144 0 2 0'"
fi

# A missing log must not abort the sum under `set -u`.
got_missing="$(sum_shards "$TMP/nope.log")"
if [ "$got_missing" = "0 0 0 0" ]; then
    PASS=$(( PASS + 1 )); echo "  ok    a missing shard log contributes zeros"
else
    FAIL=$(( FAIL + 1 )); echo "  FAIL  missing log gave '$got_missing', wanted '0 0 0 0'"
fi

echo
echo "passed: $PASS   failed: $FAIL"
[ "$FAIL" -eq 0 ]
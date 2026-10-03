#!/bin/bash
# Completeness gate for the sharded Playwright suite.
#
# Sourced by scripts/e2e.sh. Kept in its own file so it can be exercised
# directly by scripts/test_e2e_gate.sh against crafted logs, including a
# simulated worker death -- a gate that is only ever reached by a passing run
# is indistinguishable from a gate that never fires.
#
# The invariant, from DJI-595: a run that lost a worker must FAIL. It must not
# report a smaller number and leave a human to notice.

# Sum one category ("passed", "failed", "skipped", "flaky") across a shard log.
# A missing file contributes zero instead of aborting: under `set -e` a sed
# failure here would kill the runner before the gate could explain itself.
count_category() {
    [ -f "$1" ] || { echo 0; return 0; }
    sed -n "s/^ *\([0-9][0-9]*\) $2\b.*/\1/p" "$1" | awk '{s += $1} END {print s + 0}'
}

# Did any shard abandon tests because a worker died before running them?
shard_abandoned_tests() {
    grep -aq 'did not run' "$1"
}

# ---------------------------------------------------------------------------
# The skip allowlist
# ---------------------------------------------------------------------------
# Every skip the suite is allowed to make, and the precondition each one names.
# This is an allowlist, not an assertion: a NEW skip fails the run, and so does
# a DECLARED skip that starts running -- in that case the manifest is stale and
# real coverage changed without anyone saying so. Both directions are strict on
# purpose, because "2 skipped" being stable is exactly what makes it readable.
#
#   10g lidarr_wanted  -- the provider is not registered in the e2e environment
#   10i local_file     -- needs a real FILE on disk; /api/test/create-dir only
#                         makes directories, so there is nothing to point at
#
# Keyed on test TITLE, never file:line -- line numbers move whenever the spec
# is edited, and a manifest keyed on them rots on any touch.
DECLARED_SKIPS="10g. Create watchlist with source_type lidarr_wanted
10i. Create watchlist with source_type local_file"

# Titles Playwright reported as skipped, across every shard log.
#
# The list reporter marks these `-  258 [chromium] <U+203A> path:line <U+203A>
# suite <U+203A> title`, so the title is the text after the LAST separator.
# That separator is a multibyte char, so it is transliterated to \001 with
# POSIX tr octal escapes and split on with awk -- no locale or GNU-only
# assumption, and the regex does not have to match UTF-8 bytes.
skipped_titles() {
    local f
    for f in "$@"; do
        [ -f "$f" ] || continue
        # The `{ ... || true; }` is load-bearing: a shard containing no skips
        # makes grep exit 1, and under `set -euo pipefail` that would abort the
        # whole run mid-listing rather than simply contributing no titles.
        tr '\342\200\272' '\001' < "$f" \
            | { grep -aE '^[[:space:]]*-[[:space:]]+[0-9]+[[:space:]]' || true; } \
            | awk -F'\001' 'NF > 1 { t = $NF; gsub(/^[[:space:]]+|[[:space:]]+$/, "", t); if (t != "") print t }'
    done
}

# Verify the skipped set is EXACTLY the declared set. Prints the reason to
# stderr and returns non-zero otherwise.
verify_declared_skips() {
    local observed declared extra missing
    observed="$(skipped_titles "$@" | sort -u)"
    declared="$(printf '%s\n' "$DECLARED_SKIPS" | sort -u)"

    extra="$(comm -23 <(printf '%s\n' "$observed" | grep -v '^$') <(printf '%s\n' "$declared" | grep -v '^$'))"
    missing="$(comm -13 <(printf '%s\n' "$observed" | grep -v '^$') <(printf '%s\n' "$declared" | grep -v '^$'))"

    if [ -n "$extra" ]; then
        echo "undeclared skip(s) -- add them to DECLARED_SKIPS in scripts/e2e_gate.sh with their precondition:" >&2
        printf '  + %s\n' "$extra" >&2
        return 1
    fi
    if [ -n "$missing" ]; then
        echo "declared skip(s) that now RUN -- remove them from DECLARED_SKIPS; coverage changed:" >&2
        printf '  - %s\n' "$missing" >&2
        return 1
    fi
    return 0
}

# Totals across every shard log, as "passed failed skipped flaky".
# A log that does not exist yet contributes zero rather than failing the read.
sum_shards() {
    local passed=0 failed=0 skipped=0 flaky=0 log
    for log in "$@"; do
        [ -f "$log" ] || continue
        passed=$(( passed + $(count_category "$log" passed) ))
        failed=$(( failed + $(count_category "$log" failed) ))
        skipped=$(( skipped + $(count_category "$log" skipped) ))
        flaky=$(( flaky + $(count_category "$log" flaky) ))
    done
    echo "$passed $failed $skipped $flaky"
}

# verify_suite_complete <expected> <shard-log>...
#
# Exit 0 when the suite is provably complete; exit 1 with a reason on stderr
# otherwise. `expected` comes from `playwright test --list`, never a constant:
# a hard-coded expected count is the same brittleness DJI-592 just removed.
verify_suite_complete() {
    local expected="$1"; shift
    local passed failed skipped flaky executed log abandoned=0

    read -r passed failed skipped flaky <<< "$(sum_shards "$@")"
    executed=$(( passed + failed + skipped + flaky ))

    for log in "$@"; do
        [ -f "$log" ] || continue
        if shard_abandoned_tests "$log"; then
            abandoned=1
        fi
    done

    if [ "$abandoned" -ne 0 ]; then
        echo "a shard reported tests that did not run -- a worker died mid-suite" >&2
        return 1
    fi
    if [ "$executed" -ne "$expected" ]; then
        echo "only ${executed} of ${expected} declared tests reported a result (missing $(( expected - executed )))" >&2
        return 1
    fi
    if [ "$failed" -ne 0 ]; then
        echo "${failed} test(s) failed" >&2
        return 1
    fi
    if ! verify_declared_skips "$@"; then
        return 1
    fi
    return 0
}
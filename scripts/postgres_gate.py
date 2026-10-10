#!/usr/bin/env python3
"""Postgres gate: run the tests that need a database, and refuse a run where they skipped.

Why this exists (ADR 0004): `go test ./...` with no DATABASE_URL is green while
every test listed in REQUIRED below skips itself. setupPostgresForLocks,
setupPostgresForMigration and TestLockManager all t.Skip when the URL is absent,
unreachable, or not Postgres, and a skip reports as neither pass nor fail -- so a
regression in an advisory lock, a claim, or a LISTEN/NOTIFY wakeup can reach
master behind a green tick. This gate points those packages at a real Postgres
and then asserts each required test PASSED. A skip is not a pass, and a required
test that stops running is a failure too.

Two rules, and the difference from scripts/e2e_gate.sh's DECLARED_SKIPS is
deliberate. There, a declared skip that starts running is also a failure. Here it
cannot be: the ALLOWED_SKIPS entries are conditional on the runner's filesystem
(a host where `Foo` and `foo` are the same directory cannot observe a
case-variant folder split), so requiring them to skip would turn a green run red
on a case-sensitive runner. They are an allowlist -- extra skips are refused, a
declared skip that runs is fine -- and this comment is the only place that
asymmetry is allowed to be an accident.

Usage:
    DATABASE_URL=postgres://user:pass@host:5432/db python3 scripts/postgres_gate.py

Exit codes:
    0  every required test passed, no undeclared skips
    1  a violation (a required test skipped, did not run, or failed; an
       undeclared skip; a package that failed to build or run)
    2  no usable Postgres URL -- the gate refuses rather than reporting green
"""

import collections
import json
import os
import subprocess
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BACKEND = os.path.join(REPO_ROOT, "backend")

# The schema has to exist before the package run, or the two migration tests skip
# themselves: TestArtistProvenanceMigration_Postgres bails with "monitored_artists
# does not exist in this database" and TestBootstrapEnrolledAtMigration_Postgres
# with "users does not exist", both of which are silent skips. They are created
# by running the migrator production uses (database.Migrate), which is also the
# cheapest proof that the schema path works on this driver.
PRIME_PACKAGE = "./internal/database"
PRIME_TEST = "^TestMigrate_Postgres$"
PRIME_TEST_NAME = "TestMigrate_Postgres"

# Tests that MUST report `pass` once a database is available. Derived, not
# guessed: with DATABASE_URL unset each of these reports `skip`, and with a
# Postgres it reports `pass` (internal/database: 16 of 179 tests, measured
# 2026-10-09). A new test that needs a database and skips must be added here --
# if it skips without being listed, the undeclared-skip rule refuses the run.
REQUIRED = {
    "./internal/database": [
        "TestArtistProvenanceMigration_Postgres",
        "TestArtistProvenanceMigration_PreservesExistingRows",
        "TestBootstrapEnrolledAtMigration_Postgres",
        "TestBootstrapEnrolledAtMigration_PreservesExistingRows",
        "TestGetScopeLockKey",
        "TestLockManager",
        "TestMigrate_Postgres",
        "TestMigrate_PostgresIdempotent",
        "TestNewLockManager_Postgres",
        "TestPostgresLockManager_AcquireTryLock",
        "TestPostgresLockManager_AcquireTryLock_Duplicate",
        "TestPostgresLockManager_ClosedConnFreesLock",
        "TestPostgresLockManager_ExclusiveAcrossSessions",
        "TestPostgresLockManager_ReleaseDropsSessionLock",
        "TestPostgresLockManager_ReleaseLock",
        "TestPostgresLockManager_ReleaseUnknownKeyIsError",
    ],
    "./cmd/worker": [
        "TestListenNotifyInterop",
    ],
    "./internal/services": [
        "TestArtistTrackingService",
    ],
    # internal/api has no database-gated test (nothing in it skips without a
    # DATABASE_URL), so it declares none -- but it is still run here, and every
    # skip it reports is therefore undeclared by construction. See the "why" on
    # its entry in RUNS.
    "./internal/api": [],
}

# Skips these packages are allowed to report, with the precondition each names.
# Nothing is listed today: every one of these packages is skip-free under
# Postgres on this project's machines, and the entries above are the reason to
# keep it that way. If a legitimate environment-dependent skip appears, add it
# here with its reason rather than loosening the check.
ALLOWED_SKIPS = {
    "./internal/database": {},
    "./cmd/worker": {},
    "./internal/services": {},
}

# The rest of the package set the gate covers. internal/api has no
# database-gated test, but its stats endpoints are the aggregate SQL ADR 0004
# calls out, it is skip-free under Postgres, and it costs ~26s (measured).
#
# internal/services is run under a -run filter: the full package drags in
# ffmpeg/yt-dlp/filesystem skips whose presence depends on the runner, and the
# one test here that needs a database is the one the gate is about.
# "why" is required on any run whose REQUIRED list is empty, and is worth
# reading on the rest: it is the sentence that stops the next person deleting a
# run they cannot see the point of.
RUNS = [
    {
        "package": "./internal/database",
        "timeout": "10m",
        "why": "advisory locks, the migration tests, and the migrator itself",
    },
    {
        "package": "./cmd/worker",
        "timeout": "10m",
        "why": "the LISTEN/NOTIFY wakeup path, which polls would otherwise mask",
    },
    {
        "package": "./internal/api",
        "timeout": "20m",
        "why": "no database-gated test, but its stats endpoints are the aggregate "
               "SQL ADR 0004 calls out, and it is skip-free here (~26s)",
    },
    {
        "package": "./internal/services",
        "timeout": "20m",
        "filter": "^TestArtistTrackingService$",
        "why": "the one service test that opens DATABASE_URL; the full package would "
               "drag in runner-dependent ffmpeg/yt-dlp/filesystem skips",
    },
]


class RunResult(object):
    """One `go test -json` invocation, reduced to what the rules need."""

    def __init__(self, package, exit_code, observed, output):
        self.package = package
        self.exit_code = exit_code
        self.observed = observed          # {test name: set of pass/skip/fail}
        self.output = output              # {test name: [output lines]}

    def status(self, test):
        actions = self.observed.get(test, set())
        if "fail" in actions:
            return "fail"
        if "skip" in actions:
            return "skip"
        if "pass" in actions:
            return "pass"
        return "absent"

    def skipped(self):
        return [t for t in self.observed if "skip" in self.observed[t]]

    def failed(self):
        return [t for t in self.observed if "fail" in self.observed[t]]

    def skip_reason(self, test):
        """The test's own words before it skipped -- usually the precondition.

        `go test -json` attaches both the reason and the `--- SKIP:` summary line
        to the same test, and the summary comes last, so taking the last line
        verbatim reports "--- SKIP: TestMigrate_Postgres (0.01s)" -- which names
        nothing. Measured on a database that did not exist.
        """
        for line in reversed([ln.strip() for ln in self.output.get(test, []) if ln.strip()]):
            if line.startswith(("--- ", "=== ")):
                continue
            return line
        return ""


def run_go_test(package, filter_expr=None, timeout="10m", want_json=True):
    """Run one package. Returns (exit_code, raw_output_text)."""
    cmd = ["go", "test", package, "-count=1", "-timeout", timeout]
    if filter_expr:
        cmd += ["-run", filter_expr]
    if want_json:
        cmd += ["-json"]
    # `-count=1` above is deliberate and load-bearing: a cached PASS from a run
    # against a different DATABASE_URL is the same class of lie as a skip.
    proc = subprocess.run(cmd, cwd=BACKEND, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT)
    return proc.returncode, proc.stdout.decode("utf-8", "replace")


def parse_go_test_json(text):
    """Reduce `go test -json` output to {test: actions} and {test: [output]}."""
    observed = collections.defaultdict(set)
    output = collections.defaultdict(list)
    for line in text.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            event = json.loads(line)
        except ValueError:
            continue
        test = event.get("Test")
        action = event.get("Action")
        if not test:
            continue
        if action in ("pass", "skip", "fail"):
            observed[test].add(action)
        elif action == "output":
            output[test].append(event.get("Output", ""))
    return dict(observed), dict(output)


def prime_verdict(result):
    """Decide what the schema-prep run means. Returns (exit_code, message).

    Checking only `go test`'s exit status here is the trap this gate exists for,
    and its own negative control found it: TestMigrate_Postgres skips when
    DATABASE_URL is unreachable or not Postgres (it reports
    "Failed to connect to database"), a skipped test exits 0, and the gate then
    announced "schema prepared" against a database that did not exist. A skip is
    not a pass, and the priming step is where that has to be enforced first or
    every required test downstream skips for the same unexamined reason.
    """
    status = result.status(PRIME_TEST_NAME)
    if status == "pass":
        return 0, None
    if status == "skip":
        reason = result.skip_reason(PRIME_TEST_NAME) or "no reason given"
        return 2, ("DATABASE_URL is set but the migrator skipped: %s\n"
                   "  Nothing answered on that URL, or it is not Postgres. Starting the "
                   "service (or creating the database) is the fix; refusing to report "
                   "on a run whose required tests would all skip for the same reason."
                   % reason)
    return 1, ("could not prepare the schema: %s reported %s"
               % (PRIME_TEST_NAME, status))


def verify(result, required, allowed_skips):
    """Every rule, as a list of violation strings. Empty means the run is honest."""
    problems = []

    for test in required:
        status = result.status(test)
        if status == "absent":
            problems.append("%s: %s did not run at all (renamed, deleted, or the "
                            "package failed to build)" % (result.package, test))
        elif status == "skip":
            reason = result.skip_reason(test)
            problems.append("%s: %s SKIPPED -- a skip is not a pass%s"
                            % (result.package, test, (": " + reason) if reason else ""))
        elif status == "fail":
            problems.append("%s: %s FAILED" % (result.package, test))

    for test in result.skipped():
        if test in required:
            continue  # already reported, with the test's own reason
        if test not in allowed_skips:
            reason = result.skip_reason(test)
            problems.append("%s: %s skipped without being declared in ALLOWED_SKIPS%s"
                            % (result.package, test, (": " + reason) if reason else ""))

    if result.exit_code != 0:
        failed = result.failed()
        if failed:
            problems.append("%s: go test exited %d; failing: %s"
                            % (result.package, result.exit_code, ", ".join(sorted(failed))))
        else:
            problems.append("%s: go test exited %d without reporting a failing test "
                            "(build failure or timeout?)" % (result.package, result.exit_code))

    return problems


def main():
    url = os.environ.get("DATABASE_URL", "").strip()
    if not url:
        sys.stderr.write(
            "postgres_gate: DATABASE_URL is not set.\n"
            "  This gate exists because the tests it covers skip when it is absent, "
            "and a skip is not a pass.\n"
            "  Point it at a throwaway Postgres (the job in .github/workflows/ci.yml "
            "starts one) and re-run.\n"
            "  Refusing to report on a run that could not have executed the tests.\n")
        return 2
    if not url.startswith(("postgres://", "postgresql://")):
        sys.stderr.write(
            "postgres_gate: DATABASE_URL is not a Postgres URL (%s...).\n"
            "  The point of the gate is the Postgres driver; a SQLite path would "
            "skip every required test.\n" % url.split(":", 1)[0])
        return 2

    print("postgres_gate: target=%s" % _redact(url))

    prime_code, prime_text = run_go_test(PRIME_PACKAGE, PRIME_TEST, "10m")
    prime_observed, prime_output = parse_go_test_json(prime_text)
    verdict, message = prime_verdict(
        RunResult(PRIME_PACKAGE, prime_code, prime_observed, prime_output))
    if verdict:
        sys.stderr.write("postgres_gate: %s\n" % message)
        if verdict == 1:
            sys.stderr.write("".join(prime_text.splitlines(True)[-25:]))
            sys.stderr.write("  The migration tests skip when the tables they alter are "
                             "absent, so the gate stops here rather than reporting on them.\n")
        return verdict
    print("postgres_gate: schema prepared via %s (the production migrator)" % PRIME_PACKAGE)

    results = []
    for run in RUNS:
        package = run["package"]
        code, text = run_go_test(package, run.get("filter"), run.get("timeout", "10m"))
        observed, output = parse_go_test_json(text)
        results.append(RunResult(package, code, observed, output))

    problems = []
    print("")
    print("%-24s %8s %8s %8s %8s" % ("package", "required", "passed", "skipped", "failed"))
    for result in results:
        required = REQUIRED.get(result.package, [])
        passed = sum(1 for t in required if result.status(t) == "pass")
        skipped = len(result.skipped())
        failed = len(result.failed())
        print("%-24s %8d %8d %8d %8d" % (result.package, len(required), passed, skipped, failed))
        problems += verify(result, required, ALLOWED_SKIPS.get(result.package, {}))

    # Why each package is in the gate. A row of zeros invites the question
    # "what is this doing here", and the answer belongs in the log rather than
    # in a file the reader would have to open.
    print("")
    for run in RUNS:
        if run.get("why"):
            print("  %s: %s" % (run["package"], run["why"]))

    if problems:
        print("")
        print("postgres_gate: REFUSED -- %d problem(s):" % len(problems))
        for problem in problems:
            print("  * %s" % problem)
        return 1

    total_required = sum(len(REQUIRED.get(r.package, [])) for r in results)
    print("")
    print("postgres_gate: ok -- %d required tests passed against Postgres, no undeclared skips"
          % total_required)
    return 0


def _redact(url):
    """Never print the password in a CI log."""
    if "@" not in url:
        return url
    scheme, rest = url.split("://", 1)
    creds, host = rest.split("@", 1)
    user = creds.split(":", 1)[0]
    return "%s://%s:***@%s" % (scheme, user, host)


if __name__ == "__main__":
    sys.exit(main())

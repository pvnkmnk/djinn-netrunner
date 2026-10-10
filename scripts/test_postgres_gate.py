#!/usr/bin/env python3
"""Offline tests for scripts/postgres_gate.py — no database, no `go test`.

Run: python scripts/test_postgres_gate.py

What is covered is the logic that fails *quietly* rather than crashing: a run
where a required test skipped, where it never ran, where an undeclared skip
appeared, or where the package died before reporting anything. Every one of
those is fed to the real gate as crafted `go test -json`, because a gate proven
only by a passing run is indistinguishable from a gate that never fires -- the
same reason scripts/test_e2e_gate.sh exists for scripts/e2e_gate.sh.

The live contract (does Postgres actually make these tests pass) is exercised by
the gate itself against a real database; that is the CI job's job, not this
file's.
"""

import importlib.util
import io
import json
import os
import re
import sys
import unittest
from unittest import mock

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(HERE)


def load_gate():
    spec = importlib.util.spec_from_file_location(
        "postgres_gate", os.path.join(HERE, "postgres_gate.py")
    )
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


gate = load_gate()

PACKAGE = "./internal/database"


def events(*specs):
    """Build `go test -json` text from (test, action, output) triples."""
    lines = []
    for test, action, output in specs:
        if output:
            lines.append(json.dumps({"Action": "output", "Test": test, "Output": output + "\n"}))
        lines.append(json.dumps({"Action": action, "Test": test}))
    # A package-level event carries no Test key; the parser must ignore it.
    lines.append(json.dumps({"Action": "pass", "Package": "pkg"}))
    return "\n".join(lines) + "\n"


def result_for(specs, exit_code=0, package=PACKAGE):
    observed, output = gate.parse_go_test_json(events(*specs))
    return gate.RunResult(package, exit_code, observed, output)


class ParseTests(unittest.TestCase):
    def test_reduces_events_to_actions_per_test(self):
        observed, output = gate.parse_go_test_json(
            events(("TestA", "pass", ""), ("TestB", "skip", "DATABASE_URL not set"))
        )
        self.assertEqual(observed["TestA"], {"pass"})
        self.assertEqual(observed["TestB"], {"skip"})
        self.assertEqual(gate.RunResult("p", 0, observed, output).skip_reason("TestB"),
                         "DATABASE_URL not set")

    def test_the_skip_reason_ignores_the_summary_line(self):
        """Found live: with a database that did not exist, the reason came out
        as "--- SKIP: TestMigrate_Postgres (0.01s)", which names nothing."""
        text = "\n".join([
            json.dumps({"Action": "output", "Test": "TestSkip",
                        "Output": "    gate_test.go:21: Failed to connect to database: refused\n"}),
            json.dumps({"Action": "output", "Test": "TestSkip",
                        "Output": "--- SKIP: TestSkip (0.01s)\n"}),
            json.dumps({"Action": "skip", "Test": "TestSkip"}),
        ])
        observed, output = gate.parse_go_test_json(text)
        result = gate.RunResult("p", 0, observed, output)
        self.assertEqual(result.skip_reason("TestSkip"),
                         "gate_test.go:21: Failed to connect to database: refused")

    def test_tolerates_non_json_noise(self):
        """A build error line is not JSON, and must not crash the parser or
        be mistaken for a result."""
        text = "# pkg\n./x.go:3:2: undefined: thing\n" + events(("TestA", "pass", ""))
        observed, _ = gate.parse_go_test_json(text)
        self.assertEqual(set(observed), {"TestA"})


class VerifyTests(unittest.TestCase):
    REQUIRED = ["TestRequired"]
    ALLOWED = {"TestAllowed": "conditional on the runner's filesystem"}

    def verify(self, result):
        return gate.verify(result, self.REQUIRED, self.ALLOWED)

    def test_clean_run_is_accepted(self):
        problems = self.verify(result_for([("TestRequired", "pass", "")]))
        self.assertEqual(problems, [])

    def test_required_test_that_skipped_is_refused_and_quotes_its_reason(self):
        problems = self.verify(result_for(
            [("TestRequired", "skip", "Failed to connect to database: dial tcp: refused")]))
        self.assertEqual(len(problems), 1)
        self.assertIn("SKIPPED", problems[0])
        self.assertIn("a skip is not a pass", problems[0])
        self.assertIn("dial tcp: refused", problems[0])

    def test_required_test_that_never_ran_is_refused(self):
        problems = self.verify(result_for([("TestSomethingElse", "pass", "")]))
        self.assertEqual(len(problems), 1)
        self.assertIn("did not run", problems[0])

    def test_required_test_that_failed_is_refused(self):
        problems = self.verify(result_for([("TestRequired", "fail", "")], exit_code=1))
        self.assertTrue(any("FAILED" in p for p in problems))

    def test_undeclared_skip_is_refused(self):
        problems = self.verify(result_for(
            [("TestRequired", "pass", ""),
             ("TestSurprise", "skip", "some new precondition")]))
        self.assertEqual(len(problems), 1)
        self.assertIn("ALLOWED_SKIPS", problems[0])
        self.assertIn("TestSurprise", problems[0])

    def test_declared_skip_may_skip_or_run(self):
        """The documented asymmetry with e2e_gate.sh: these entries are
        conditional on the host, so running is not a failure."""
        for action in ("skip", "pass"):
            problems = self.verify(result_for(
                [("TestRequired", "pass", ""), ("TestAllowed", action, "")]))
            self.assertEqual(problems, [], "action=%s" % action)

    def test_package_that_died_without_a_failing_test_is_refused(self):
        """A build failure or timeout reports no failing test at all, which is
        the signal -- not a keyword search over output that can legitimately
        contain compiler words."""
        problems = self.verify(gate.RunResult(PACKAGE, 1, {}, {}))
        self.assertEqual(len(problems), 2)  # required test absent + non-zero exit
        self.assertTrue(any("without reporting a failing test" in p for p in problems))

    def test_failing_test_is_named_in_the_exit_code_violation(self):
        problems = self.verify(result_for(
            [("TestRequired", "pass", ""), ("TestOther", "fail", "")], exit_code=1))
        self.assertTrue(any("TestOther" in p for p in problems))


class PrimeVerdictTests(unittest.TestCase):
    """The schema-prep run, which is the first place 'a skip is not a pass' has
    to be enforced. Its own negative control found this: an unreachable
    DATABASE_URL makes TestMigrate_Postgres skip, `go test` exits 0, and a gate
    that only checked the exit status announced 'schema prepared' against a
    database that did not exist."""

    def verdict(self, action, exit_code=0, reason=""):
        observed, output = gate.parse_go_test_json(
            events((gate.PRIME_TEST_NAME, action, reason)))
        return gate.prime_verdict(
            gate.RunResult(gate.PRIME_PACKAGE, exit_code, observed, output))

    def test_a_pass_proceeds(self):
        self.assertEqual(self.verdict("pass"), (0, None))

    def test_a_skip_on_an_unreachable_url_is_refused_as_configuration(self):
        code, message = self.verdict(
            "skip", reason="Failed to connect to database: dial tcp 127.0.0.1:59999: refused")
        self.assertEqual(code, 2)
        self.assertIn("migrator skipped", message)
        self.assertIn("dial tcp 127.0.0.1:59999: refused", message)

    def test_a_failing_prime_is_a_violation_not_a_configuration_error(self):
        code, message = self.verdict("fail", exit_code=1)
        self.assertEqual(code, 1)
        self.assertIn("could not prepare the schema", message)

    def test_a_prime_that_never_ran_is_a_violation(self):
        observed, output = gate.parse_go_test_json(events(("TestSomethingElse", "pass", "")))
        code, _message = gate.prime_verdict(
            gate.RunResult(gate.PRIME_PACKAGE, 0, observed, output))
        self.assertEqual(code, 1)


class RefusalTests(unittest.TestCase):
    def test_no_database_url_refuses_with_two(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            stderr = io.StringIO()
            with mock.patch.object(sys, "stderr", stderr):
                code = gate.main()
        self.assertEqual(code, 2)
        self.assertIn("DATABASE_URL is not set", stderr.getvalue())
        self.assertIn("a skip is not a pass", stderr.getvalue())

    def test_sqlite_url_refuses_with_two(self):
        with mock.patch.dict(os.environ, {"DATABASE_URL": "netrunner.db"}, clear=True):
            stderr = io.StringIO()
            with mock.patch.object(sys, "stderr", stderr):
                code = gate.main()
        self.assertEqual(code, 2)
        self.assertIn("not a Postgres URL", stderr.getvalue())

    def test_a_password_never_reaches_the_log(self):
        redacted = gate._redact("postgres://user:s3cret@host:5432/db?sslmode=disable")
        self.assertNotIn("s3cret", redacted)
        self.assertIn("user:***@host:5432", redacted)


class RegistryTests(unittest.TestCase):
    """The gate's own consistency: a stale manifest is how this kind of check
    rots into a green tick."""

    def test_every_run_declares_required_tests_or_a_reason(self):
        for run in gate.RUNS:
            self.assertIn(run["package"], gate.REQUIRED,
                          "%s is gated but declares no required tests" % run["package"])
            if not gate.REQUIRED[run["package"]]:
                self.assertTrue(run.get("why"),
                                "%s is gated with no required tests and no reason" % run["package"])

    def test_a_run_filter_cannot_exclude_its_own_required_tests(self):
        """A filter that does not match a required name would make the gate
        demand a test its own run never executes -- 'did not run' forever."""
        for run in gate.RUNS:
            if not run.get("filter"):
                continue
            pattern = re.compile(run["filter"])
            for test in gate.REQUIRED[run["package"]]:
                self.assertTrue(pattern.match(test),
                                "%s: filter %r excludes required test %s"
                                % (run["package"], run["filter"], test))

    def test_a_test_cannot_be_required_and_allowed_to_skip(self):
        for package, required in gate.REQUIRED.items():
            allowed = gate.ALLOWED_SKIPS.get(package, {})
            overlap = set(required) & set(allowed)
            self.assertEqual(overlap, set(),
                             "%s: %s is both required and allowed to skip" % (package, overlap))

    def test_the_gate_covers_the_packages_that_gate_on_database_url(self):
        """Every package holding a test that keys on DATABASE_URL must be in the
        run list, or a new database-gated test can appear in a package nothing
        checks."""
        covered = {run["package"] for run in gate.RUNS}
        for package in gate.REQUIRED:
            self.assertIn(package, covered)

    def test_every_required_test_exists_in_the_sources(self):
        """An anchor, not a name: a renamed or deleted test must fail here with
        the reason, rather than surfacing later as a confusing 'did not run'."""
        names = set()
        backend = os.path.join(REPO_ROOT, "backend")
        for root, _dirs, files in os.walk(backend):
            for name in files:
                if not name.endswith("_test.go"):
                    continue
                path = os.path.join(root, name)
                with io.open(path, encoding="utf-8", errors="replace") as handle:
                    for line in handle:
                        match = re.match(r"func (Test\w+)\(", line)
                        if match:
                            names.add(match.group(1))
        for package, required in sorted(gate.REQUIRED.items()):
            for test in required:
                self.assertIn(test, names,
                              "%s: %s is required by the gate but no longer exists in "
                              "the sources" % (package, test))


if __name__ == "__main__":
    unittest.main(verbosity=2)

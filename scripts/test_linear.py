#!/usr/bin/env python3
"""Offline tests for scripts/linear.py — no network, no Linear key.

Run: python scripts/test_linear.py

What is covered here is the logic that silently misbehaves rather than
crashing: the token search order, the query shapes (whose field names and
variable types Linear validates strictly), the partial-success trap, the
throttle, and the no-op path. The live contract is exercised separately by
`linear.py contract`, which is opt-in because it needs a real workspace.
"""

import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import types
import unittest

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))


def load_cli():
    spec = importlib.util.spec_from_file_location(
        "linear_cli", os.path.join(HERE, "linear.py")
    )
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


cli = load_cli()


class FakeApi:
    """Records calls and replays canned responses, so no network is touched."""

    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []
        self.remaining = "2500"
        self.limit = "2500"
        self.throttles = 0

    def throttle(self):
        # Counted so a test can assert the budget check ran before a write.
        self.throttles += 1

    def gql(self, query, variables=None, retries=4):
        self.calls.append((query, variables or {}))
        if not self.responses:
            raise AssertionError("unexpected extra call: %s" % query[:80])
        return self.responses.pop(0)


class TestTokenResolution(unittest.TestCase):
    def test_reads_token_file_first(self):
        with tempfile.NamedTemporaryFile("w", suffix=".token", delete=False) as fh:
            fh.write("lin_api_fromfile" + "\n")
            path = fh.name
        old_file = os.environ.get("LINEAR_TOKEN_FILE")
        old_env = os.environ.get("LINEAR_API_KEY")
        try:
            os.environ["LINEAR_TOKEN_FILE"] = path
            os.environ["LINEAR_API_KEY"] = "lin_api_fromenv"
            self.assertEqual(cli.token(), "lin_api_fromfile")
        finally:
            _restore("LINEAR_TOKEN_FILE", old_file)
            _restore("LINEAR_API_KEY", old_env)
            os.unlink(path)

    def test_falls_back_to_env(self):
        with tempfile.NamedTemporaryFile("w", suffix=".token", delete=False) as fh:
            fh.write("")
            path = fh.name
        old_file = os.environ.get("LINEAR_TOKEN_FILE")
        old_env = os.environ.get("LINEAR_API_KEY")
        try:
            os.environ["LINEAR_TOKEN_FILE"] = path
            os.environ["LINEAR_API_KEY"] = "lin_api_fromenv"
            self.assertEqual(cli.token(), "lin_api_fromenv")
        finally:
            _restore("LINEAR_TOKEN_FILE", old_file)
            _restore("LINEAR_API_KEY", old_env)
            os.unlink(path)

    def test_trims_surrounding_whitespace(self):
        with tempfile.NamedTemporaryFile("w", suffix=".token", delete=False) as fh:
            fh.write("  lin_api_padded  \n\n")
            path = fh.name
        old_file = os.environ.get("LINEAR_TOKEN_FILE")
        old_env = os.environ.pop("LINEAR_API_KEY", None)
        try:
            os.environ["LINEAR_TOKEN_FILE"] = path
            self.assertEqual(cli.token(), "lin_api_padded")
        finally:
            _restore("LINEAR_TOKEN_FILE", old_file)
            _restore("LINEAR_API_KEY", old_env)
            os.unlink(path)


def _restore(key, value):
    if value is None:
        os.environ.pop(key, None)
    else:
        os.environ[key] = value


class TestProjectBodyNoOp(unittest.TestCase):
    """An unchanged body must not write — a blind rewrite is the risk."""

    def _args(self, body_text, extra=()):
        ns = types.SimpleNamespace(
            project="P-TEST",
            file=None,
            stdout=False,
            dry_run=False,
            verbose=False,
        )
        for kv in extra:
            setattr(ns, kv[0], kv[1])
        tmp = tempfile.NamedTemporaryFile("w", suffix=".md", delete=False, encoding="utf-8")
        tmp.write(body_text)
        tmp.close()
        ns.file = tmp.name
        return ns, tmp.name

    def test_identical_body_writes_nothing(self):
        body = "# same\n\ntext"
        api = FakeApi([
            {"project": {"id": "uuid-1", "identifier": "P-TEST", "name": "T",
                         "description": "d", "content": body,
                         "documentContent": {"content": body}}},
        ])
        args, path = self._args(body)
        try:
            rc = cli.cmd_project_body(api, args)
        finally:
            os.unlink(path)
        self.assertEqual(rc, 0)
        self.assertEqual(len(api.calls), 1, "no second call => no write")

    def test_dry_run_never_writes(self):
        current = "# old\n"
        api = FakeApi([
            {"project": {"id": "uuid-1", "identifier": "P-TEST", "name": "T",
                         "description": "d", "content": current,
                         "documentContent": {"content": current}}},
        ])
        args, path = self._args("# new\n", extra=(("dry_run", True),))
        try:
            rc = cli.cmd_project_body(api, args)
        finally:
            os.unlink(path)
        self.assertEqual(rc, 0)
        self.assertEqual(len(api.calls), 1, "--dry-run must not issue a mutation")
        self.assertNotIn("projectUpdate", api.calls[0][0])


class TestWriteTargetsContentOnly(unittest.TestCase):
    """The bug this CLI exists to prevent: writing `description` by mistake."""

    def test_mutation_sends_content_and_never_description(self):
        current = "# old\n"
        api = FakeApi([
            {"project": {"id": "uuid-1", "identifier": "P-TEST", "name": "T",
                         "description": "SENTINEL", "content": current,
                         "documentContent": {"content": current}}},
            {"data": {}},  # projectUpdate success
            {"project": {"id": "uuid-1", "identifier": "P-TEST", "name": "T",
                         "description": "SENTINEL", "content": "# new\n",
                         "documentContent": {"content": "# new\n"}}},
        ])
        tmp = tempfile.NamedTemporaryFile("w", suffix=".md", delete=False, encoding="utf-8")
        tmp.write("# new\n")
        tmp.close()
        args = types.SimpleNamespace(project="P-TEST", file=tmp.name,
                                     stdout=False, dry_run=False, verbose=False)
        try:
            cli.cmd_project_body(api, args)
        finally:
            os.unlink(tmp.name)

        mutation = api.calls[1]
        self.assertIn("projectUpdate", mutation[0])
        self.assertNotIn("description:", mutation[0])
        self.assertEqual(mutation[1].get("c"), "# new\n")
        self.assertEqual(mutation[1].get("id"), "uuid-1")


class TestIssueListQueryShape(unittest.TestCase):
    """Linear validates field names and variable types before executing."""

    def test_filter_goes_inside_filter_argument(self):
        api = FakeApi([
            {"project": {"id": "proj-uuid", "identifier": "P-DJI-28", "name": "n",
                         "teams": {"nodes": [{"id": "team-uuid", "key": "DJI"}]}}},
            {"issues": {"nodes": []}},
        ])
        args = types.SimpleNamespace(project="P-DJI-28", state="Backlog",
                                     limit=10, json=False, verbose=False)
        cli.cmd_issue_list(api, args)

        query = api.calls[1][0]
        # `project`/`state` are IssueFilter fields, not Query.issues arguments.
        self.assertIn("filter:{project:{id:{eq:$pid}},state:{name:{eq:$state}}}", query)
        self.assertNotIn("issues(first:10, project:", query)
        # project.id is ID-typed; declaring $pid as String is a validation error.
        self.assertIn("$pid:ID", query)

    def test_no_filters_means_no_filter_argument(self):
        api = FakeApi([{"issues": {"nodes": []}}])
        args = types.SimpleNamespace(project=None, state=None,
                                     limit=5, json=False, verbose=False)
        cli.cmd_issue_list(api, args)
        self.assertNotIn("filter:", api.calls[0][0])
        self.assertNotIn("$pid", api.calls[0][0])


class TestWorkflowStateLookup(unittest.TestCase):
    def test_resolves_state_by_name_and_sends_uuid(self):
        api = FakeApi([
            {"workflowStates": {"nodes": [
                {"id": "uuid-backlog", "name": "Backlog", "type": "backlog"},
                {"id": "uuid-done", "name": "Done", "type": "completed"},
            ]}},
            {"issueUpdate": {"success": True, "issue": {
                "identifier": "DJI-1", "state": {"name": "Done"}}}},
        ])
        args = types.SimpleNamespace(issue="DJI-1", state="done", verbose=False)
        cli.cmd_state(api, args)
        # Case-insensitive match, but the UUID is what goes on the wire.
        self.assertEqual(api.calls[1][1]["s"], "uuid-done")

    def test_unknown_state_is_refused_before_any_write(self):
        api = FakeApi([
            {"workflowStates": {"nodes": [
                {"id": "uuid-backlog", "name": "Backlog", "type": "backlog"}]}},
        ])
        args = types.SimpleNamespace(issue="DJI-1", state="Shipped", verbose=False)
        with self.assertRaises(SystemExit):
            cli.cmd_state(api, args)
        self.assertEqual(len(api.calls), 1, "must not issue a mutation")


class TestIssueCreateTeamIds(unittest.TestCase):
    def test_uses_team_ids_array_not_team_id(self):
        api = FakeApi([
            {"project": {"id": "proj-uuid", "identifier": "P-DJI-28", "name": "n",
                         "teams": {"nodes": [{"id": "team-uuid", "key": "DJI"}]}}},
            {"issueCreate": {"success": True,
                             "issue": {"identifier": "DJI-999", "url": "u"}}},
        ])
        args = types.SimpleNamespace(title="t", description=None,
                                     description_file=None, project="P-DJI-28",
                                     verbose=False)
        cli.cmd_issue(api, args)
        query, variables = api.calls[1]
        self.assertIn("teamIds: $teams", query)
        self.assertNotIn("teamId: $team", query)
        self.assertEqual(variables["teams"], ["team-uuid"])


class TestResolveProject(unittest.TestCase):
    def test_direct_identifier_lookup_is_one_call(self):
        api = FakeApi([
            {"project": {"id": "proj-uuid", "identifier": "P-DJI-28", "name": "n",
                         "teams": {"nodes": [{"id": "team-uuid", "key": "DJI"}]}}},
        ])
        got = cli.resolve_project(api, "P-DJI-28")
        self.assertEqual(got["id"], "proj-uuid")
        self.assertEqual(len(api.calls), 1)
        # ProjectFilter has no `identifier` field, so a name fragment needs the
        # separate name search -- never an identifier filter.
        self.assertNotIn("identifier:", api.calls[0][0])

    def test_falls_back_to_name_search(self):
        api = FakeApi([
            {"project": None},
            {"projects": {"nodes": [
                {"id": "proj-uuid", "identifier": "P-DJI-28", "name": "Playtest",
                 "teams": {"nodes": [{"id": "team-uuid", "key": "DJI"}]}}]}},
        ])
        got = cli.resolve_project(api, "Playtest")
        self.assertEqual(got["id"], "proj-uuid")
        self.assertIn("name:{contains:$q}", api.calls[1][0])


class TestApiGuards(unittest.TestCase):
    """The failure shapes that read as success if unguarded."""

    def test_partial_success_errors_array_is_fatal(self):
        api = cli.Api("tok")
        captured = {}

        def fake_urlopen(req, timeout=None):
            class R:
                headers = {"X-RateLimit-Requests-Limit": "2500",
                           "X-RateLimit-Requests-Remaining": "2499"}

                def read(self):
                    return json.dumps({
                        "data": {"project": None},
                        "errors": [{"message": "Entity not found",
                                    "extensions": {"code": "ENTITY_NOT_FOUND"}}],
                    }).encode()

                def __enter__(self):
                    return self

                def __exit__(self, *a):
                    return False
            return R()

        import urllib.request
        real = urllib.request.urlopen
        urllib.request.urlopen = fake_urlopen
        try:
            with self.assertRaises(SystemExit):
                api.gql("{ project(id:\"x\"){ id } }")
        finally:
            urllib.request.urlopen = real

    def test_throttle_only_fires_near_the_ceiling(self):
        api = cli.Api("tok")
        api.remaining = "2500"
        api.throttle()  # must return immediately, no sleep

        slept = []
        real_sleep = cli.time.sleep
        cli.time.sleep = lambda s: slept.append(s)
        try:
            api.remaining = "2"
            api.throttle()
            api.remaining = "not-a-number"
            api.throttle()
        finally:
            cli.time.sleep = real_sleep
        self.assertEqual(len(slept), 1, "only the near-ceiling case sleeps")
        self.assertGreater(slept[0], 0)

    def test_unparseable_remaining_does_not_crash(self):
        api = cli.Api("tok")
        api.remaining = None
        api.throttle()


class TestDiffBehaviour(unittest.TestCase):
    def test_identical_returns_false_and_prints_nothing(self):
        import contextlib

        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            changed = cli.show_diff("same\n", "same\n", "body")
        self.assertFalse(changed)
        self.assertEqual(buf.getvalue(), "")

    def test_change_is_reported_with_both_labels(self):
        import contextlib

        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            changed = cli.show_diff("a\n", "b\n", "body")
        self.assertTrue(changed)
        out = buf.getvalue()
        self.assertIn("current", out)
        self.assertIn("proposed", out)


class TestWebhookRegisterGuards(unittest.TestCase):
    """Linear will not deliver to a non-https or localhost endpoint, and the
    retry budget is three attempts -- so a bad URL must fail before we create
    anything rather than leaving a webhook that silently never fires."""

    def test_rejects_plain_http(self):
        args = types.SimpleNamespace(
            url="http://example.com/hook", types=None, print_secret=False,
            verbose=False)
        with self.assertRaises(SystemExit):
            cli.cmd_webhook_register(FakeApi([]), args)

    def test_rejects_localhost(self):
        args = types.SimpleNamespace(
            url="https://localhost:8787/hook", types=None, print_secret=False,
            verbose=False)
        with self.assertRaises(SystemExit):
            cli.cmd_webhook_register(FakeApi([]), args)

    def test_rejects_loopback_ip(self):
        args = types.SimpleNamespace(
            url="https://127.0.0.1/hook", types=None, print_secret=False,
            verbose=False)
        with self.assertRaises(SystemExit):
            cli.cmd_webhook_register(FakeApi([]), args)


class TestDrainTokenResolution(unittest.TestCase):
    def test_reads_drain_token_file(self):
        with tempfile.NamedTemporaryFile("w", suffix=".token", delete=False) as fh:
            fh.write("drain-from-file\n")
            path = fh.name
        old_file = os.environ.get("LINEAR_DRAIN_TOKEN_FILE")
        old_env = os.environ.get("LINEAR_DRAIN_TOKEN")
        try:
            os.environ["LINEAR_DRAIN_TOKEN_FILE"] = path
            os.environ["LINEAR_DRAIN_TOKEN"] = "drain-from-env"
            self.assertEqual(cli._drain_token(), "drain-from-file")
        finally:
            _restore("LINEAR_DRAIN_TOKEN_FILE", old_file)
            _restore("LINEAR_DRAIN_TOKEN", old_env)
            os.unlink(path)

    def test_missing_token_is_refused(self):
        old_file = os.environ.get("LINEAR_DRAIN_TOKEN_FILE")
        old_env = os.environ.pop("LINEAR_DRAIN_TOKEN", None)
        try:
            os.environ["LINEAR_DRAIN_TOKEN_FILE"] = "/nonexistent/drain-token"
            with self.assertRaises(SystemExit):
                cli._drain_token()
        finally:
            _restore("LINEAR_DRAIN_TOKEN_FILE", old_file)
            _restore("LINEAR_DRAIN_TOKEN", old_env)


class TestWebhookResourceTypes(unittest.TestCase):
    """Linear declares resourceTypes as [String!]!, not the enum type that
    introspection suggests, so a query naming [WebhookResourceType!] is a
    validation error before it ever reaches the API."""

    def test_registration_declares_string_list(self):
        api = FakeApi([
            {"webhookCreate": {"success": True, "webhook": {
                "id": "wh1", "label": None, "url": "https://w.example",
                "enabled": True}}},
            {"webhook": {"id": "wh1", "label": None,
                         "url": "https://w.example", "secret": "lin_wh_x",
                         "enabled": True}},
        ])
        args = types.SimpleNamespace(
            url="https://w.example/hook", types=None, print_secret=False,
            verbose=False)
        cli.cmd_webhook_register(api, args)
        query, variables = api.calls[0]
        self.assertIn("$types:[String!]!", query)
        self.assertNotIn("WebhookResourceType", query)
        self.assertEqual(sorted(variables["types"]), ["Comment", "Issue", "Project"])
        self.assertTrue(variables["all"], "public teams must be included")

    def test_explicit_types_are_passed_through(self):
        api = FakeApi([
            {"webhookCreate": {"success": True, "webhook": {
                "id": "wh1", "label": None, "url": "https://w.example",
                "enabled": True}}},
            {"webhook": {"id": "wh1", "label": None,
                         "url": "https://w.example", "secret": "lin_wh_x",
                         "enabled": True}},
        ])
        args = types.SimpleNamespace(
            url="https://w.example/hook", types=["Issue"], print_secret=False,
            verbose=False)
        cli.cmd_webhook_register(api, args)
        self.assertEqual(api.calls[0][1]["types"], ["Issue"])


# A body with several lines, an em dash and a non-ASCII char, so the capture is
# exercised on multi-byte output as well as on line endings.
BODY_FIXTURE = (
    "# heading" + chr(10)
    + chr(10)
    + "a paragraph with an em dash " + chr(8212) + " and e-acute " + chr(233)
    + chr(10)
    + "| a | b |" + chr(10)
    + "| -- | -- |" + chr(10)
    + "final line" + chr(10)
)

# Runs the real --stdout path in a child process. The body arrives on stdin as
# BYTES because sys.stdin in text mode would apply universal-newline translation
# and quietly repair the very corruption under test.
_STDOUT_RUNNER = """\
import importlib.util, sys, types

spec = importlib.util.spec_from_file_location("linear_cli", sys.argv[1])
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)

body = sys.stdin.buffer.read().decode("utf-8")


class FakeApi:
    def __init__(self):
        self.calls = []

    def throttle(self):
        pass

    def gql(self, query, variables=None, retries=4):
        self.calls.append(query)
        return {"project": {"id": "u", "identifier": "P-TEST", "name": "T",
                            "description": "d", "content": body,
                            "documentContent": {"content": body}}}


args = types.SimpleNamespace(project="P-TEST", file=None, stdout=True,
                             dry_run=False, verbose=False)
cli.cmd_project_body(FakeApi(), args)
"""


class TestStdoutIsByteExact(unittest.TestCase):
    """`--stdout` must emit the stored body byte for byte.

    Deliberately a SUBPROCESS. The defect is a text-stream newline translation,
    which only happens when stdout is a real stream; a redirect_stdout(StringIO)
    sink cannot observe it, so an in-process test here would be a guard nothing
    can violate. These run against a real OS pipe.

    Which test catches which defect, stated plainly:

    * The two byte-level tests are the regression guard, but only on Windows --
      that is the only platform where the translation happens at all.
    * The round-trip test below does NOT catch it. It passes against the
      unfixed code, because --file reads with io.open(..., encoding="utf-8")
      and universal newlines repairs CRLF on the way in. It is kept because it
      pins the round-trip contract: the day --file is hardened to newline=""
      (the same class of fix applied to the read side), a capture that is not
      byte-exact starts rewriting the whole body, and this test is what would
      notice. It guards a future change, not the present bug.
    """

    def _capture(self, body):
        tmpdir = tempfile.mkdtemp()
        runner = os.path.join(tmpdir, "runner.py")
        with io.open(runner, "w", encoding="utf-8", newline="") as fh:
            fh.write(_STDOUT_RUNNER)
        return subprocess.run(
            [sys.executable, runner, os.path.join(HERE, "linear.py")],
            input=body.encode("utf-8"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

    def test_capture_carries_no_carriage_return(self):
        proc = self._capture(BODY_FIXTURE)
        self.assertEqual(
            proc.returncode, 0, proc.stderr.decode("utf-8", "replace")
        )
        self.assertNotIn(
            b"\r",
            proc.stdout,
            "a CR in the capture means the body would be rewritten in CRLF "
            "the next time it is fed back to --file",
        )

    def test_capture_is_byte_identical_to_the_stored_body(self):
        proc = self._capture(BODY_FIXTURE)
        self.assertEqual(proc.stdout, BODY_FIXTURE.encode("utf-8"))

    def test_capture_fed_back_to_file_is_a_no_op(self):
        """Capture, land the bytes as a redirect would, feed them to --file.

        Proven to pass against the UNFIXED code too -- see the class docstring.
        Asserting only "it is a no-op" would be a guard that cannot fail, so
        this one also checks the captured file is the size the bytes imply.
        """
        proc = self._capture(BODY_FIXTURE)
        self.assertEqual(
            proc.returncode, 0, proc.stderr.decode("utf-8", "replace")
        )
        # Land the captured bytes exactly as a shell redirect would.
        path = os.path.join(tempfile.mkdtemp(), "captured.md")
        with open(path, "wb") as fh:
            fh.write(proc.stdout)

        api = FakeApi([{
            "project": {"id": "uuid-1", "identifier": "P-TEST", "name": "T",
                        "description": "d", "content": BODY_FIXTURE,
                        "documentContent": {"content": BODY_FIXTURE}},
        }])
        args = types.SimpleNamespace(
            project="P-TEST", file=path, stdout=False, dry_run=False, verbose=False,
        )
        self.assertEqual(
            os.path.getsize(path), len(proc.stdout),
            "the landed file must hold exactly the captured bytes",
        )
        rc = cli.cmd_project_body(api, args)
        self.assertEqual(rc, 0)
        self.assertEqual(
            len(api.calls), 1,
            "capturing --stdout and feeding it back must not write; a second "
            "call means the capture differed from what Linear stores",
        )
        self.assertNotIn("projectUpdate", api.calls[0][0])

if __name__ == "__main__":
    unittest.main(verbosity=2)
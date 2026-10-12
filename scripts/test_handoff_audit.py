#!/usr/bin/env python3
"""Offline tests for scripts/handoff_audit.py  -  no network, no `go test`.

Run: python3 scripts/test_handoff_audit.py

The auditor's whole value is its FALSE verdict, so that is what these tests
pin: for every claim shape there is a fixture document whose claim is right
(the control) and one whose claim is wrong by exactly one degree  -  a count off
by one, a sha that does not resolve, a size off by a byte, a line number that
is stale, a claimed-clean diff that is not empty  -  and the wrong one must be
reported FALSE. A tool proven only on a correct document is indistinguishable
from one that never looks.

The fixture is a real throwaway git repository under a temp dir, because every
count, size and line check reads the tree; faking that would test the fixture
instead of the auditor. Command and PR checks are exercised through a Shell
subclass that returns canned output, so no `go` and no `gh` is needed  -  and the
read-only allowlist is tested directly, because a document is untrusted input
and the auditor must refuse to run what it cannot justify running.
"""

import contextlib
import importlib.util
import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(HERE)


def load_auditor():
    spec = importlib.util.spec_from_file_location(
        "handoff_audit", os.path.join(HERE, "handoff_audit.py")
    )
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


audit_mod = load_auditor()


GO_MOD = """module example.com/x

go 1.27.0

require (
\tgolang.org/x/net v0.60.0
\tgithub.com/gofiber/fiber/v3 v3.5.0
)
"""

MAIN_GO = """package main

func cookie() {
\tSecure: c.Scheme() == "https",
}
"""

# The same shape one directory deeper, because that is where the real failure
# was: a bare `spotify_auth.go` is not under backend/ directly, so the resolver
# has to fall through to the tracked paths -- where a stale copy of this
# repository under `.aaa-stale/` (another session's worktree, still holding the
# pre-fix line) must not be reached first.
SPOTIFY_FIXED = """package api

func cookie() {
\tSecure: c.Scheme() == "https",
}
"""
SPOTIFY_STALE = """package api

func cookie() {
\tSecure: c.Protocol() == "https",
}
"""
GITIGNORE = ".aaa-stale/\n"

# The doc's dotted path prefix is what makes it resolvable from the tree root.
BIG_MD = "x" * 4095 + "\n"  # exactly 4096 bytes


class Fixture:
    """A throwaway repository with a known working-tree shape.

    Known by construction, so the control document's numbers are hand-written
    rather than read back out of the code under test:
      tracked changes: 3  (M backend/main.go, M README.md, D backend/codemap.md)
      modified backend/*.go: 1
      codemap.md deletions: 1
      untracked files: 1  (docs/NEW.md)
    """

    def __init__(self):
        self.root = tempfile.mkdtemp(prefix="handoff-audit-")
        # The document lives outside the audited tree on purpose: a file inside
        # it would be an untracked entry and would change the very counts the
        # control document asserts.
        self.docdir = tempfile.mkdtemp(prefix="handoff-docs-")
        self.git = ["git", "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false"]
        self._write("backend/go.mod", GO_MOD)
        self._write("backend/main.go", MAIN_GO)
        self._write("backend/codemap.md", "stale map\n")
        self._write("backend/internal/api/spotify_auth.go", SPOTIFY_FIXED)
        self._write("README.md", "before\n")
        self._write("docs/BIG.md", BIG_MD)
        self._write("docs/OLD.md", "old\n")
        self._write(".gitignore", GITIGNORE)
        self._commit()
        # The working-tree shape the counters describe.
        self._write("backend/main.go", MAIN_GO + "\n// touched\n")
        self._write("README.md", "before\nafter\n")
        os.remove(os.path.join(self.root, "backend", "codemap.md"))
        self._write("docs/NEW.md", "brand new\n")
        # Ignored, so it changes no counter, and untracked, so no git command
        # would ever find it -- reachable only by walking into it. It is
        # deliberately *shallower* than the real file, so a resolver that only
        # sorts walk hits by depth still picks this one: the tracked path is
        # what the test is about.
        self._write(".aaa-stale/spotify_auth.go", SPOTIFY_STALE)

    def _write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8", newline="") as fh:
            fh.write(text)

    def _commit(self):
        env = dict(os.environ)
        env.update({
            "GIT_AUTHOR_NAME": "audit", "GIT_AUTHOR_EMAIL": "audit@example.com",
            "GIT_COMMITTER_NAME": "audit", "GIT_COMMITTER_EMAIL": "audit@example.com",
        })
        subprocess.run(self.git + ["init", "-q"], cwd=self.root, check=True, capture_output=True, env=env)
        subprocess.run(self.git + ["add", "-A"], cwd=self.root, check=True, capture_output=True, env=env)
        subprocess.run(self.git + ["commit", "-q", "-m", "fixture"], cwd=self.root,
                       check=True, capture_output=True, env=env)
        # The branch the control document names, at the commit it names.
        subprocess.run(self.git + ["branch", "docs/example"], cwd=self.root,
                       check=True, capture_output=True, env=env)

    def head(self):
        env = dict(os.environ)
        # The full object name, not --short: a short sha can be all digits,
        # which the audit's is_sha() would read as not-a-sha at all.
        out = subprocess.run(self.git + ["rev-parse", "HEAD"], cwd=self.root,
                             capture_output=True, text=True, env=env)
        return out.stdout.strip()

    def doc(self, text):
        path = os.path.join(self.docdir, "HANDOFF.md")
        with open(path, "w", encoding="utf-8", newline="") as fh:
            fh.write(text)
        return path

    def shell(self, run_commands=False):
        shell = audit_mod.Shell(self.root, run_commands=run_commands)
        shell.absence_target = "both"
        return shell

    def audit(self, text, shell=None):
        path = self.doc(text)
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            doc = audit_mod.Doc(fh.read(), name=path)
        return audit_mod.audit(doc, shell or self.shell())

    def cleanup(self):
        shutil.rmtree(self.root, ignore_errors=True)
        shutil.rmtree(self.docdir, ignore_errors=True)


def verdicts(claims, kind):
    return [c["verdict"] for c in claims if c["kind"] == kind]


def only(claims, kind):
    found = [c for c in claims if c["kind"] == kind]
    assert len(found) == 1, "expected exactly one %s claim, got %d" % (kind, len(found))
    return found[0]


class ControlDocument(unittest.TestCase):
    """Every claim true: the shape a document should have when it is done."""

    def setUp(self):
        self.fx = Fixture()

    def tearDown(self):
        self.fx.cleanup()

    def control_text(self):
        return "\n".join([
            "# Handoff",
            "",
            "Checkout is on branch `docs/example` (head `%s`), parent `%s`." % (
                self.fx.head(), self.fx.head()),
            "",
            "There are 3 tracked files modified in the working tree, including",
            "1 modified `backend/` `.go` files and 1 `codemap.md` deletions, plus",
            "1 untracked files.",
            "",
            "go.mod versions (go 1.27.0, `golang.org/x/net v0.60.0`,",
            "`github.com/gofiber/fiber/v3 v3.5.0`).",
            "",
            "`docs/BIG.md` is 4,096 bytes.",
            "",
            "The registration is in `backend/main.go` line 4",
            "(`Secure: c.Scheme() == \"https\",`), and the stale anchors",
            "`zzz-absent-token` return no matches anywhere.",
        ])

    def test_control_document_is_all_confirmed(self):
        claims = self.fx.audit(self.control_text())
        self.assertEqual([c for c in claims if c["verdict"] == audit_mod.FALSE], [],
                         "control document must not produce a FALSE claim")
        kinds = {c["kind"] for c in claims}
        self.assertLessEqual(
            {"revision", "branch", "count", "version", "size", "line-ref", "absence"}, kinds,
            "every shape the control document exercises must be extracted")
        self.assertGreaterEqual(len(claims), 10)

    def test_branch_head_claim_matches_the_fixture(self):
        claims = self.fx.audit(self.control_text())
        self.assertEqual(verdicts(claims, "branch"), [audit_mod.CONFIRMED])
        self.assertEqual(verdicts(claims, "line-ref"), [audit_mod.CONFIRMED])


class FalseClaimsAreCaught(unittest.TestCase):
    """One degree off, one shape at a time. Each of these must be FALSE."""

    def setUp(self):
        self.fx = Fixture()

    def tearDown(self):
        self.fx.cleanup()

    def test_unresolvable_revision(self):
        claims = self.fx.audit("The migration sits at `deadbee1234` on disk.")
        self.assertEqual(verdicts(claims, "revision"), [audit_mod.FALSE])

    def test_wrong_branch_head(self):
        claims = self.fx.audit("On branch `docs/example` (head `abc1234`).")
        self.assertEqual(verdicts(claims, "branch"), [audit_mod.FALSE])
        self.assertIn("is", only(claims, "branch")["evidence"])

    def test_count_off_by_one(self):
        claims = self.fx.audit("There are 4 tracked files modified in the working tree.")
        claim = only(claims, "count")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("tree has 3", claim["evidence"])

    def test_a_bare_basename_resolves_to_the_tracked_file(self):
        """Measured live: `spotify_auth.go line 73` resolved into another
        session's stale worktree copy of this repo, so a pre-fix line was
        CONFIRMED as current. The tree's own file is what the number means."""
        claims = self.fx.audit(
            "The bug is at `spotify_auth.go` line 4 (`Secure: c.Protocol() == \"https\",`).")
        claim = only(claims, "line-ref")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn(os.path.join("internal", "api", "spotify_auth.go"), claim["evidence"])
        self.assertNotIn("stale", claim["evidence"])

    def test_short_module_name_resolves_by_suffix(self):
        """`x/net v0.60.0` names the module go.mod requires as `golang.org/x/net`."""
        claims = self.fx.audit("go.mod versions (`x/net v0.60.0`).")
        claim = only(claims, "version")
        self.assertEqual(claim["verdict"], audit_mod.CONFIRMED)
        self.assertIn("golang.org/x/net", claim["evidence"])

    def test_short_module_name_with_the_wrong_version(self):
        claims = self.fx.audit("go.mod versions (`fiber/v3 v3.4.0`).")
        claim = only(claims, "version")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("github.com/gofiber/fiber/v3 v3.5.0", claim["evidence"])

    def test_a_reference_does_not_turn_following_prose_into_a_snippet(self):
        """Measured live: `internal/api/auth.go:246`; the adaptor call is at
        `cmd/server/main.go:134` made the words *between* the two references a
        "snippet", and the line was reported FALSE for not containing them.
        Both references are correct and must be CONFIRMED."""
        claims = self.fx.audit("See `main.go:3` and the marker: "
                               "`backend/main.go:4` is the second site.")
        verdicts_here = [c["verdict"] for c in claims if c["kind"] == "line-ref"]
        self.assertEqual(verdicts_here, [audit_mod.CONFIRMED, audit_mod.CONFIRMED])

    def test_codemap_deletion_count_off(self):
        claims = self.fx.audit("That leaves 2 `codemap.md` deletions uncommitted.")
        self.assertEqual(verdicts(claims, "count"), [audit_mod.FALSE])

    def test_wrong_byte_size(self):
        claims = self.fx.audit("`docs/BIG.md` is 4,097 bytes.")
        claim = only(claims, "size")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("4096", claim["evidence"])

    def test_size_claim_for_a_file_that_is_not_there(self):
        claims = self.fx.audit("The draft `docs/NETRUNNERLITE.md` is 7,793 bytes.")
        self.assertEqual(verdicts(claims, "size"), [audit_mod.FALSE])

    def test_dependency_version_that_is_not_in_go_mod(self):
        claims = self.fx.audit("go.mod versions (`golang.org/x/net v0.61.0`).")
        self.assertEqual(verdicts(claims, "version"), [audit_mod.FALSE])

    def test_go_directive_that_is_not_in_go_mod(self):
        claims = self.fx.audit("`go 1.25.13` is what the module declares.")
        self.assertEqual(verdicts(claims, "version"), [audit_mod.FALSE])

    def test_stale_line_number_is_caught_and_relocated(self):
        """The quietest false claim: the file is right, the number is not."""
        claims = self.fx.audit(
            "The cookie flag is at `backend/main.go` line 3 "
            "(`Secure: c.Scheme() == \"https\",`).")
        claim = only(claims, "line-ref")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("line 4, not 3", claim["evidence"])

    def test_line_ref_beyond_the_end_of_the_file(self):
        claims = self.fx.audit("See `backend/main.go:900` for the rest.")
        self.assertEqual(verdicts(claims, "line-ref"), [audit_mod.FALSE])

    def test_claimed_clean_diff_that_is_not_empty(self):
        claims = self.fx.audit("`git diff HEAD -- README.md` is **empty** on this branch.")
        self.assertEqual(verdicts(claims, "absence"), [audit_mod.FALSE])

    def test_absence_that_is_present_in_the_audited_document(self):
        claims = self.fx.audit("`zzz-absent-token` return no matches.\n\n`zzz-absent-token` again.\n")
        self.assertEqual(verdicts(claims, "absence"), [audit_mod.FALSE])
        self.assertIn("another line", only(claims, "absence")["evidence"])

    def test_absence_that_is_present_in_the_tree(self):
        claims = self.fx.audit("These stale anchors return no matches: `Secure: c.Scheme() == \"https\",`.")
        claim = only(claims, "absence")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("the tree", claim["evidence"])

    def test_clean_diff_of_an_untouched_path_is_confirmed(self):
        claims = self.fx.audit("`git diff HEAD -- docs/OLD.md` is **empty** on this branch.")
        self.assertEqual(verdicts(claims, "absence"), [audit_mod.CONFIRMED])


class UnverifiableIsNotConfirmed(unittest.TestCase):
    """Commands and PR state are never assumed good, and --strict says so."""

    def setUp(self):
        self.fx = Fixture()

    def tearDown(self):
        self.fx.cleanup()

    def test_command_claims_are_unverifiable_without_run_commands(self):
        claims = self.fx.audit("`go build ./...` -> **exit 0** today.")
        self.assertEqual(verdicts(claims, "command"), [audit_mod.UNVERIFIABLE])
        self.assertIn("--run-commands", only(claims, "command")["evidence"])

    def test_pr_claims_are_unverifiable_without_a_network(self):
        claims = self.fx.audit("PR #359 is open and its `test` job FAILs.")
        self.assertEqual(verdicts(claims, "pr"), [audit_mod.UNVERIFIABLE])

    def test_strict_turns_unverifiable_into_a_failure(self):
        path = self.fx.doc("`go build ./...` -> **exit 0** today.\n")
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(audit_mod.main([path, "--root", self.fx.root]), 0)
            self.assertEqual(audit_mod.main([path, "--root", self.fx.root, "--strict"]), 1)

    def test_a_false_claim_exits_non_zero(self):
        path = self.fx.doc("There are 99 tracked files modified in the working tree.\n")
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(audit_mod.main([path, "--root", self.fx.root]), 1)

    def test_a_document_with_nothing_to_check_refuses(self):
        path = self.fx.doc("Just prose, no claims at all.\n")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            self.assertEqual(audit_mod.main([path, "--root", self.fx.root]), 2)
        self.assertIn("not a pass", buf.getvalue())


class FakedRunChecks(unittest.TestCase):
    """The command and PR checks, against canned output instead of a runner."""

    class FakeShell(audit_mod.Shell):
        def __init__(self, root, canned, **kw):
            super().__init__(root, run_commands=True, **kw)
            self.canned = canned
            self.calls = []

        def _run(self, argv, cwd):
            self.calls.append(argv)
            for key, value in self.canned.items():
                if " ".join(argv).startswith(key):
                    return value
            return None, "no canned answer for %s" % (" ".join(argv))

    def setUp(self):
        self.fx = Fixture()

    def tearDown(self):
        self.fx.cleanup()

    def test_command_exit_code_mismatch_is_false(self):
        shell = self.FakeShell(self.fx.root, {"go build": (1, "boom\n")})
        claims = self.fx.audit("`go build ./...` -> **exit 0**.", shell)
        claim = only(claims, "command")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("exit 1", claim["evidence"])

    def test_command_exit_code_match_is_confirmed(self):
        shell = self.FakeShell(self.fx.root, {"go vet": (0, "")})
        claims = self.fx.audit("`go vet ./...` -> **exit 0**.", shell)
        self.assertEqual(verdicts(claims, "command"), [audit_mod.CONFIRMED])

    def test_failing_test_count_is_checked_not_just_the_exit_code(self):
        out = "--- FAIL: TestOne (0.00s)\n--- FAIL: TestTwo (0.01s)\nFAIL\n"
        shell = self.FakeShell(self.fx.root, {"go test": (1, out)})
        claims = self.fx.audit(
            "`go test ./internal/api` -> **exit 1 with exactly ONE failing test**", shell)
        claim = only(claims, "command")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("2 `--- FAIL`", claim["evidence"])

    def test_one_failing_test_as_claimed_is_confirmed(self):
        out = "--- FAIL: TestSpotifyAuthHandler_LoginCookieSecure (0.01s)\nFAIL\n"
        shell = self.FakeShell(self.fx.root, {"go test": (1, out)})
        claims = self.fx.audit(
            "`go test ./internal/api` -> **exit 1 with exactly one failing test**", shell)
        self.assertEqual(verdicts(claims, "command"), [audit_mod.CONFIRMED])

    def test_pr_head_mismatch_is_false(self):
        canned = {"gh pr view": (0, '{"state":"OPEN","headRefOid":"ffffffffffffffff","mergeable":"MERGEABLE"}')}
        shell = self.FakeShell(self.fx.root, canned)
        claims = self.fx.audit("PR #359 @ `%s` is open." % self.fx.head(), shell)
        claim = only(claims, "pr")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("document claims", claim["evidence"])

    def test_pr_check_status_mismatch_is_false(self):
        """The stale 'still pending' line a handoff is most likely to carry."""
        canned = {
            "gh pr view": (0, '{"state":"OPEN","headRefOid":"%s","mergeable":"MERGEABLE"}' % self.fx.head()),
            "gh pr checks": (0, "test\tpass\t6m28s\thttps://example/1\nintegration\tpass\t3m8s\thttps://example/2\n"),
        }
        shell = self.FakeShell(self.fx.root, canned)
        claims = self.fx.audit("PR #%s @ `%s`: `test` FAIL still." % (359, self.fx.head()), shell)
        claim = only(claims, "pr")
        self.assertEqual(claim["verdict"], audit_mod.FALSE)
        self.assertIn("document says FAIL, gh says pass", claim["evidence"])

    def test_pr_check_status_agreement_is_confirmed(self):
        canned = {
            "gh pr view": (0, '{"state":"OPEN","headRefOid":"%s","mergeable":"MERGEABLE"}' % self.fx.head()),
            "gh pr checks": (0, "test\tpass\t6m28s\thttps://example/1\n"),
        }
        shell = self.FakeShell(self.fx.root, canned)
        claims = self.fx.audit("PR #%s @ `%s`: the `test` job passes." % (359, self.fx.head()), shell)
        self.assertEqual(verdicts(claims, "pr"), [audit_mod.CONFIRMED])


class ReadOnlyAllowlist(unittest.TestCase):
    """A handoff is untrusted input: the auditor refuses what it cannot run."""

    def setUp(self):
        self.fx = Fixture()
        self.shell = self.fx.shell(run_commands=True)

    def tearDown(self):
        self.fx.cleanup()

    def test_shell_syntax_is_never_executed(self):
        argv, cwd, code, reason = self.shell.command("go build ./... && rm -rf /")
        self.assertIsNone(argv)
        self.assertIn("shell syntax", reason)

    def test_a_writing_git_verb_is_refused(self):
        argv, cwd, code, reason = self.shell.command("git reset --hard origin/master")
        self.assertIsNone(argv)
        self.assertIn("not read-only", reason)

    def test_an_arbitrary_program_is_refused(self):
        argv, cwd, code, reason = self.shell.command("curl http://example.com")
        self.assertIsNone(argv)
        self.assertIn("allowlist", reason)

    def test_a_write_gh_subcommand_is_refused(self):
        argv, cwd, code, reason = self.shell.command("gh pr merge 359 --squash")
        self.assertIsNone(argv)
        self.assertIn("gh pr view|checks", reason)

    def test_no_document_command_can_write(self):
        """Whatever a document says, no `git` write verb is reachable."""
        for line in ("git push origin HEAD", "git checkout master", "git add -A",
                     "git commit -m x", "git clean -fd", "git stash"):
            argv, _, _, _ = self.shell.command(line)
            self.assertIsNone(argv, "%s must not be runnable" % line)

    def test_a_refused_command_is_reported_not_silently_skipped(self):
        shell = self.fx.shell(run_commands=True)
        claims = self.fx.audit("`go build ./... && rm -rf /` -> **exit 0**.", shell)
        claim = only(claims, "command")
        self.assertEqual(claim["verdict"], audit_mod.UNVERIFIABLE)
        self.assertIn("shell syntax", claim["evidence"])


class ClaimIndex(unittest.TestCase):
    """The CHECKS index is the reader's contract about what is covered."""

    def test_every_check_row_names_a_kind_the_auditor_emits(self):
        kinds = {row[0] for row in audit_mod.CHECKS}
        self.assertLessEqual(
            {"revision", "branch", "count", "size", "version", "command", "line-ref", "absence", "pr"},
            kinds)

    def test_extractors_are_all_reachable(self):
        self.assertEqual(len(audit_mod.EXTRACTORS), 9)
        self.assertEqual(len(audit_mod.CHECKS), len(audit_mod.EXTRACTORS))


if __name__ == "__main__":
    unittest.main(verbosity=2)

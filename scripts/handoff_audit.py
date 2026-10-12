#!/usr/bin/env python3
"""Handoff audit: re-derive a handoff document's factual claims from the tree.

A handoff written for the next agent is a document full of claims about the
repository -- branch heads, commit ids, file counts, byte sizes, dependency
versions, file:line references, command exit codes, PR states -- and the next
agent acts on them instead of re-deriving them. A wrong one is paid for twice:
the agent trusts a stale fact, then builds its next decision on top of it. Two
that cost real work here: a handoff that said the Go migration "was pushed once"
(it was not, so the remote was never re-checked) and one that named two template
files as "modified here, leave unstaged" (they were clean on this branch, so
staging them would have duplicated work another branch owned).

This is the "Verification notes" table a careful audit writes by hand, made
repeatable: it extracts the claims the document actually contains, re-derives
each from the live tree, and prints CONFIRMED / FALSE / UNVERIFIABLE with the
command that decided it. It is deliberately not a prose summarizer and not a
truth oracle -- it only checks the shapes it knows (see CHECKS at the bottom),
and every claim it cannot decide is reported as such rather than assumed good.

Usage:
    python3 scripts/handoff_audit.py <handoff.md> [--root DIR] [--run-commands]
                                     [--strict] [--json] [--absence-target doc|tree|both]

Exit codes:
    0  every checkable claim held; no FALSE claim
    1  at least one claim is FALSE (with --strict, also any UNVERIFIABLE)
    2  nothing was checked -- the document has no claims this tool understands,
       which is reported rather than printed as a pass

Safety: a handoff is untrusted input. Commands found in it are split with
shlex.split, run without a shell, and only when argv[0] is on a read-only
allowlist (`go build|vet|test`, `git diff|status|rev-parse|grep|ls-files|show|
log|cat-file`, `gh pr view|checks`, `python3`). Anything with a pipe, redirect,
`&&`, `$(...)` or a subshell is skipped and reported UNVERIFIABLE. The auditor
never runs a command that writes -- no `git` write verb is reachable.

Scope, stated so it is not mistaken for more: it reads one document and the
working tree/git index. It does not fetch (a claimed commit missing locally is
FALSE here, not "maybe on the remote"), and the PR checks need `gh` and a
network -- offline they are UNVERIFIABLE, never CONFIRMED.

It also has no notion of history: a document that describes the state *before*
a change ("the bug is `c.Protocol()` at spotify_auth.go:73") reads FALSE once
the fix has landed, because the tree is checked as it is now. That verdict is
about the line, not about the document being wrong -- which is why every FALSE
comes with the evidence that decided it, and why --strict is opt-in.
"""

import argparse
import json
import os
import re
import shlex
import subprocess
import sys

# The report quotes the document, and a handoff is prose -- em dashes, arrows,
# box-drawing comments in a pasted file listing. On a cp1252 console an
# unconfigured stdout raises UnicodeEncodeError partway through the report
# instead of printing the claim it was about. Same line as the other scripts
# here (scripts/test_postgres_gate.py, scripts/test_linear.py).
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

CONFIRMED = "CONFIRMED"
FALSE = "FALSE"
UNVERIFIABLE = "UNVERIFIABLE"

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Read-only commands a document is allowed to have re-run. `git` is restricted
# to verbs that cannot change the repository, which is why this is a tuple of
# prefixes rather than a list of binaries.
SAFE_GIT_VERBS = (
    "diff", "status", "rev-parse", "rev-list", "grep", "ls-files", "show",
    "log", "cat-file", "merge-base", "symbolic-ref", "for-each-ref",
)
SHELL_META = re.compile(r"[|&;<>`$()]|\|\||&&")


class Shell:
    """Runs the commands a check needs; the seam the offline tests replace.

    `root` is the tree the checks read, so the tests can point every check at a
    fixture repository instead of this one.
    """

    def __init__(self, root, run_commands=False, timeout=900):
        self.root = root
        self.backend = os.path.join(root, "backend")
        self.run_commands = run_commands
        self.timeout = timeout

    def _run(self, argv, cwd):
        try:
            proc = subprocess.run(
                argv, cwd=cwd, capture_output=True, text=True,
                encoding="utf-8", errors="replace", timeout=self.timeout,
            )
        except FileNotFoundError:
            return None, "%s: not found" % argv[0]
        except subprocess.TimeoutExpired:
            return None, "timeout after %ds" % self.timeout
        out = (proc.stdout or "") + (proc.stderr or "")
        return proc.returncode, out

    def git(self, *args):
        """A git call against the audited tree (never a write verb)."""
        return self._run(["git"] + list(args), self.root)

    def gh(self, *args):
        return self._run(["gh"] + list(args), self.root)

    def command(self, line):
        """Re-run a command quoted by the document, if it is safe to re-run.

        Returns (argv, cwd, returncode, output) or (None, None, None, reason).
        """
        if SHELL_META.search(line):
            return None, None, None, "not re-run: the command uses shell syntax"
        try:
            argv = shlex.split(line)
        except ValueError as exc:
            return None, None, None, "not re-run: unparsable (%s)" % exc
        if not argv:
            return None, None, None, "not re-run: empty command"
        prog = os.path.basename(argv[0])
        if prog == "go" and argv[1:2] and argv[1] in ("build", "vet", "test", "list", "version", "env"):
            cwd = self.backend if os.path.isdir(self.backend) else self.root
        elif prog == "git":
            if argv[1:2] and argv[1] in SAFE_GIT_VERBS:
                cwd = self.root
            else:
                return None, None, None, "not re-run: git %s is not read-only" % argv[1:2]
        elif prog == "gh":
            if argv[1:3] and argv[1] == "pr" and argv[2] in ("view", "checks", "diff", "list"):
                cwd = self.root
            else:
                return None, None, None, "not re-run: only `gh pr view|checks` is allowed"
        elif prog in ("python", "python3") and len(argv) > 1 and argv[1].startswith("scripts/"):
            cwd = self.root
        else:
            return None, None, None, "not re-run: %s is outside the read-only allowlist" % prog
        code, out = self._run(argv, cwd)
        return argv, cwd, code, out


class Doc:
    """The document, flattened line by line.

    Markdown links are reduced to their label (`[main.go](url)` -> `main.go`)
    because that is the form the prose reads in, and the flattening happens
    inside each line so line numbers stay exactly the document's own.
    """

    LINK = re.compile(r"\[([^\]]+)\]\(([^)]*)\)")

    def __init__(self, text, name="<doc>"):
        self.name = name
        self.raw = text
        self.orig_lines = text.split("\n")
        self.lines = [self.LINK.sub(r"\1", ln) for ln in self.orig_lines]
        self.flat = "\n".join(self.lines)

    def line_of(self, pos):
        return self.flat.count("\n", 0, pos) + 1

    def quote(self, pos, length=0):
        line = self.line_of(pos)
        return self.lines[line - 1].strip() if length == 0 else self.lines[line - 1].strip()[:120]

    def sentences(self):
        """(line_number, sentence) pairs, so a claim keeps its own line."""
        for n, line in enumerate(self.lines, start=1):
            for part in re.split(r"(?<=[.;])\s+", line):
                if part.strip():
                    yield n, part.strip()


def claim(kind, doc, pos, verdict, evidence, text=None, length=110):
    return {
        "kind": kind,
        "line": doc.line_of(pos),
        "text": (text if text is not None else doc.quote(pos))[:length],
        "verdict": verdict,
        "evidence": evidence,
    }


# --- counters ---------------------------------------------------------------

def working_tree(shell):
    """Parse `git status --porcelain` once, as the counters the doc asserts.

    Returns a dict of the shapes a handoff actually quotes, so a claim can be
    compared against a named definition rather than a re-derived one:
    tracked = every entry except `??` (untracked) and `!!` (ignored).
    """
    code, out = shell.git("status", "--porcelain")
    if code != 0:
        return None
    entries = []
    for line in out.splitlines():
        if len(line) < 4:
            continue
        status, path = line[:2], line[3:]
        if " -> " in path:  # a rename reports both sides; the target is the file
            path = path.split(" -> ", 1)[1]
        entries.append((status, path.strip()))
    tracked = [(s, p) for s, p in entries if s not in ("??", "!!")]
    return {
        "tracked_changes": len(tracked),
        "modified": len([1 for s, _ in tracked if "M" in s]),
        "deleted": len([1 for s, _ in tracked if "D" in s]),
        "untracked": len([1 for s, _ in entries if s == "??"]),
        "backend_go": len([1 for s, p in tracked if p.startswith("backend/") and p.endswith(".go")]),
        "codemap_deletions": len([1 for s, p in tracked if "D" in s and p.endswith("codemap.md")]),
        "entries": entries,
    }


def gomod(shell):
    """`backend/go.mod` as {module: version} plus the go/toolchain directives."""
    path = os.path.join(shell.backend, "go.mod")
    if not os.path.isfile(path):
        return None
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        text = fh.read()
    mods = {}
    for name, version in re.findall(r"^\s*([^\s/]+(?:/[^\s]+)+)\s+(v[^\s]+)", text, re.M):
        mods[name] = version
    go_version = re.search(r"^go\s+(\S+)", text, re.M)
    return {
        "modules": mods,
        "go": go_version.group(1) if go_version else None,
        "path": path,
    }


# --- extractors -------------------------------------------------------------
# Each extractor takes (doc, shell, cache) and yields claim dicts. A claim shape
# is added by writing one function and appending it to EXTRACTORS; the CHECKS
# list at the bottom is the reader-facing index of what is covered.

SHA = r"([0-9a-f]{7,40})"
COUNT = r"(\d[\d,]*)"

# Directories that are never the tree's own source: nested checkouts of this
# repository (`.orca/worktrees/...`, a `vendor` copy) and build output. A
# document's bare basename must not resolve into one of those.
PRUNED_DIRS = (".git", ".orca", "node_modules", "vendor", "dist", "build")


def resolve_path(shell, cache, path):
    """The tree's own file for a path a document names, or None.

    Three steps, and the order is the point. The path as written is tried under
    the root, `backend/` and `ops/`; then the *tracked* paths with that basename,
    because git knows which file in this repository it is; only then a pruned
    walk, for untracked files. Measured on a real handoff: a bare
    `spotify_auth.go line 73` resolved into
    `.orca/worktrees/djinn-netrunner/First-Session-with-Orca-Tools/backend/...`
    -- another session's stale copy of this repo, still holding the pre-fix
    `c.Protocol()` -- and the audit confirmed a claim against code that no
    longer exists. A line reference answered from a stale copy is worse than no
    answer.
    """
    files = cache.setdefault("files", {})
    if path in files:
        return files[path]
    resolved = None
    for candidate in (os.path.join(shell.root, path),
                      os.path.join(shell.backend, path),
                      os.path.join(shell.root, "ops", path)):
        if os.path.isfile(candidate):
            resolved = candidate
            break
    if resolved is None:
        base = os.path.basename(path)
        tracked = cache.get("tracked")
        if tracked is None:
            code, out = shell.git("ls-files")
            tracked = cache["tracked"] = out.splitlines() if code == 0 else []
        matches = [t for t in tracked if os.path.basename(t) == base]
        exact = [t for t in matches if t == path or t.endswith("/" + path)]
        pick = sorted(exact or matches, key=lambda t: (t.count("/"), t))
        if pick:
            resolved = os.path.join(shell.root, pick[0])
    if resolved is None:
        found = []
        for dirpath, dirnames, filenames in os.walk(shell.root):
            dirnames[:] = [d for d in dirnames if d not in PRUNED_DIRS
                           and not os.path.exists(os.path.join(dirpath, d, ".git"))]
            if base in filenames:
                found.append(os.path.join(dirpath, base))
        if found:
            resolved = sorted(found, key=lambda p: (p.count(os.sep), p))[0]
    files[path] = resolved
    return resolved


def resolve_module(mods, name):
    """The required module a document means by `x/net` or by a full path.

    A handoff writing `x/net v0.60.0` is naming the module go.mod requires as
    `golang.org/x/net`; an exact-key lookup read that as "not required" and
    reported a correct document FALSE. Suffix resolution, and only when it is
    unambiguous -- two candidates means the document's shorthand is genuinely
    unclear, which is worth saying rather than guessing.
    """
    if name in mods:
        return name, mods[name]
    candidates = [k for k in mods if k.endswith("/" + name)]
    if len(candidates) == 1:
        return candidates[0], mods[candidates[0]]
    return None, None


def is_sha(token):
    """A sha, not a CI run id or a byte count: hex and at least one letter."""
    return 6 < len(token) <= 40 and re.fullmatch(r"[0-9a-f]+", token) and re.search(r"[a-f]", token)


def extract_revisions(doc, shell, cache):
    """`deadbee` -- a commit the document tells the next agent to build on."""
    for m in re.finditer(r"`" + SHA + r"`", doc.flat):
        sha = m.group(1)
        if not is_sha(sha):
            continue
        cache.setdefault("revs", {})
        if sha not in cache["revs"]:
            full, out = shell.git("rev-parse", "--verify", "--quiet", sha + "^{commit}")
            cache["revs"][sha] = (full == 0, (out or "").strip() or "does not resolve")
        ok, detail = cache["revs"][sha]
        yield claim(
            "revision", doc, m.start(),
            CONFIRMED if ok else FALSE,
            "git rev-parse --verify %s^{commit} -> %s" % (sha, "ok" if ok else "unknown revision"),
            text="`%s`" % sha,
        )


def extract_branches(doc, shell, cache):
    """`branch` (head `deadbee`), `branch` @`deadbee`: the head must match."""
    patterns = [
        re.compile(r"`([\w][\w./-]*)`[^\n`]{0,24}?\(head\s*`" + SHA + r"`"),
        re.compile(r"`([\w][\w./-]*)`\s*@\s*`" + SHA + r"`"),
        re.compile(r"`([\w][\w./-]*)`\s+at\s+`" + SHA + r"`"),
    ]
    seen = set()
    for pattern in patterns:
        for m in pattern.finditer(doc.flat):
            branch, sha = m.group(1), m.group(2)
            if "/" not in branch or (branch, sha) in seen:
                continue
            seen.add((branch, sha))
            cache.setdefault("branches", {})
            if branch not in cache["branches"]:
                resolved = None
                for ref in (branch, "origin/" + branch):
                    code, out = shell.git("rev-parse", "--verify", "--quiet", ref)
                    if code == 0 and out.strip():
                        resolved = (ref, out.strip())
                        break
                cache["branches"][branch] = resolved
            resolved = cache["branches"][branch]
            if resolved is None:
                yield claim("branch", doc, m.start(), FALSE,
                            "neither %s nor origin/%s resolves" % (branch, branch),
                            text="`%s`" % branch)
            elif not resolved[1].startswith(sha):
                yield claim("branch", doc, m.start(), FALSE,
                            "%s is %s, not %s" % (resolved[0], resolved[1][:11], sha),
                            text="`%s` head `%s`" % (branch, sha))
            else:
                yield claim("branch", doc, m.start(), CONFIRMED,
                            "git rev-parse %s -> %s" % (resolved[0], resolved[1][:11]),
                            text="`%s` head `%s`" % (branch, sha))


COUNT_SHAPES = [
    # (regex over the flattened line, counter name, how the claim reads)
    (re.compile(COUNT + r"\s+tracked\s+files?\s+(?:modified|changed|in the working tree)"),
     "tracked_changes", "tracked changes"),
    (re.compile(r"~?" + COUNT + r"\s+(?:modified\s+)?`?backend/`?\s*`?\.go`?\s+files?"),
     "backend_go", "modified backend/**.go files"),
    (re.compile(r"~?" + COUNT + r"\s+(?:`?codemap\.md`?\s+)?(?:files?[^.\n]{0,20}?)?deletions?\s+of\s+`?codemap\.md`?"),
     "codemap_deletions", "codemap.md deletions"),
    (re.compile(r"~?" + COUNT + r"\s+`?codemap\.md`?\s+deletions?"),
     "codemap_deletions", "codemap.md deletions"),
    (re.compile(COUNT + r"\s+untracked\s+files?"), "untracked", "untracked files"),
]


def extract_counts(doc, shell, cache):
    """`106 tracked files modified`, `~70 backend/*.go files`: real counts."""
    found = [(key, label, m) for pattern, key, label in COUNT_SHAPES
             for m in pattern.finditer(doc.flat)]
    if not found:
        return  # a document with no counts is not owed a claim about them
    counts = cache.get("counts")
    if counts is None:
        counts = cache["counts"] = working_tree(shell)
    if counts is None:
        yield claim("count", doc, 0, UNVERIFIABLE, "git status --porcelain did not run")
        return
    for key, label, m in found:
        claimed = int(m.group(1).replace(",", ""))
        approx = "~" in m.group(0)[:2] or "about" in m.group(0) or "around" in m.group(0)
        observed = counts[key]
        tolerance = max(1, round(observed * 0.1))
        if observed == claimed:
            verdict = CONFIRMED
        elif approx and abs(observed - claimed) <= tolerance:
            verdict = CONFIRMED
        else:
            verdict = FALSE
        evidence = "%s: %d (tree has %d)" % (label, claimed, observed)
        if approx:
            evidence += " [approximate claim]"
        yield claim("count", doc, m.start(), verdict, evidence, text=m.group(0))


def extract_sizes(doc, shell, cache):
    """`docs/NETRUNNERLITE.md` ... 7,793 bytes: the file and its real size."""
    pattern = re.compile(
        r"`?([\w./-]+\.(?:md|go|py|sh|json|ya?ml|sql|html|js|css))`?[^.\n]{0,140}?"
        r"([\d][\d,]{2,})\s*bytes")
    for m in pattern.finditer(doc.flat):
        path, size = m.group(1), int(m.group(2).replace(",", ""))
        cache.setdefault("files", {})
        resolved = resolve_path(shell, cache, path)
        if resolved is None:
            yield claim("size", doc, m.start(), FALSE,
                        "no file named %s in the tree" % path, text=m.group(0)[:100])
            continue
        actual = os.path.getsize(resolved)
        yield claim("size", doc, m.start(),
                    CONFIRMED if actual == size else FALSE,
                    "%s is %d bytes (claimed %s)" % (os.path.relpath(resolved, shell.root), actual, m.group(2)),
                    text=m.group(0)[:100])


def extract_versions(doc, shell, cache):
    """`x/net v0.60.0`, `go 1.27.0`, `contrib/v3/websocket`: go.mod, in full."""
    gomodinfo = cache.get("gomod")
    if gomodinfo is None:
        gomodinfo = cache["gomod"] = gomod(shell)
    if gomodinfo is None:
        return
    mods, gover = gomodinfo["modules"], gomodinfo["go"]
    # A version claim: a module path and a v-tag inside one backtick span.
    for m in re.finditer(r"`([\w./-]+/[^\s`]+)\s+(v[\w.\-+]+)`", doc.flat):
        name, version = m.group(1), m.group(2)
        full, real = resolve_module(mods, name)
        if full is None:
            yield claim("version", doc, m.start(), FALSE,
                        "%s is not required by backend/go.mod" % name, text=m.group(0))
        elif real != version:
            yield claim("version", doc, m.start(), FALSE,
                        "backend/go.mod has %s %s, not %s" % (full, real, version),
                        text=m.group(0))
        else:
            yield claim("version", doc, m.start(), CONFIRMED,
                        "backend/go.mod requires %s %s" % (full, version), text=m.group(0))
    for m in re.finditer(r"`go\s+(\d+\.\d+(?:\.\d+)?)`", doc.flat):
        claimed = m.group(1)
        yield claim("version", doc, m.start(),
                    CONFIRMED if claimed == gover else FALSE,
                    "backend/go.mod go directive is %s (claimed %s)" % (gover, claimed),
                    text=m.group(0))
    # A bare module path in the versions list is a presence claim. The shape is
    # what separates a module from a file path: `docs/BIG.md` and
    # `backend/main.go` are not candidates, or a correct document would be
    # reported FALSE for naming its own files.
    for m in re.finditer(r"`([a-z0-9-]+\.[a-z]{2,}(?:/[\w.-]+)+)`(?!\s*v)", doc.flat, re.I):
        name = m.group(1)
        if "." in name.rsplit("/", 1)[-1]:
            continue  # a file extension in the last segment: a path, not a module
        full, real = resolve_module(mods, name)
        if full is not None:
            yield claim("version", doc, m.start(), CONFIRMED,
                        "backend/go.mod requires %s %s" % (full, real), text=m.group(0))
        elif re.search(r"(go\.mod|versions?|dependenc|requires)",
                       doc.flat[max(0, m.start() - 160):m.start()], re.I):
            yield claim("version", doc, m.start(), FALSE,
                        "%s is not required by backend/go.mod" % name, text=m.group(0))


EXIT_PATTERN = re.compile(r"exit(?:\s+code)?\s+([0-9]+)", re.I)
FAIL_COUNT = re.compile(r"(exactly\s+)?(one|two|\d+)\s+(?:failing test|`?--- FAIL`?)", re.I)
WORDS = {"one": 1, "two": 2, "three": 3}


def extract_commands(doc, shell, cache):
    """`go build ./...` -> exit 0, `go test ./internal/api` -> one FAIL line."""
    command_span = re.compile(r"`((?:cd\s+)?(?:GOTOOLCHAIN=\S+\s+)?(?:go|git|gh|python3?)\s[^`]{0,200})`")
    offset = 0
    for line in doc.lines:
        for m in command_span.finditer(line):
            rest = line[m.end():m.end() + 160]
            exit_match = EXIT_PATTERN.search(rest)
            fails = FAIL_COUNT.search(rest)
            if not exit_match and not fails:
                continue
            command = m.group(1).strip()
            claimed_exit = int(exit_match.group(1)) if exit_match else None
            claimed_fails = None
            if fails:
                token = fails.group(2).lower()
                claimed_fails = WORDS.get(token) or int(token)
            position = offset + m.start()
            if not shell.run_commands:
                yield claim("command", doc, position, UNVERIFIABLE,
                            "not re-run (--run-commands not passed): %s" % command,
                            text=command)
                continue
            argv, cwd, code, out = shell.command(command)
            # Both refusals carry their reason in `out`: one was never allowed to
            # run (not read-only, shell syntax), the other could not (no binary,
            # timeout). Neither is evidence, so neither may read as CONFIRMED.
            if argv is None or code is None:
                yield claim("command", doc, position, UNVERIFIABLE, out, text=command)
                continue
            observed_fails = [ln for ln in out.splitlines() if ln.strip().startswith("--- FAIL")]
            ok = True
            detail = "%s -> exit %d" % (" ".join(argv), code)
            if claimed_exit is not None:
                expect_ok = claimed_exit == 0
                if expect_ok != (code == 0):
                    ok = False
            if claimed_fails is not None:
                if len(observed_fails) != claimed_fails:
                    ok = False
                    detail += " (%d `--- FAIL` line(s), claimed %d)" % (len(observed_fails), claimed_fails)
                else:
                    detail += " (%d `--- FAIL` line(s), as claimed)" % len(observed_fails)
            yield claim("command", doc, position, CONFIRMED if ok else FALSE, detail, text=command)
        offset += len(line) + 1


LINE_REF = re.compile(
    r"`?([\w./-]+\.(?:go|py|sh|sql|md|ya?ml|html|js|css|toml|json))`?[:\s]+(?:line\s*)?~?(\d+)")


def extract_line_refs(doc, shell, cache):
    """`spotify_auth.go` line 73: the file must have it, and it must be it.

    A stale line number is the quietest false claim in a handoff -- the file
    exists, the number is close, and the next agent reads the wrong code. So
    when the document quotes a snippet on the same line (or the two after it),
    the snippet is checked against that line, and a match nearby is reported
    with the line it is really on.
    """
    for match in LINE_REF.finditer(doc.flat):
        path, number = match.group(1), int(match.group(2))
        resolved = resolve_path(shell, cache, path)
        snippet = None
        tail = doc.flat[match.end():match.end() + 160]
        span = re.search(r"`([^`]{6,120})`", tail)
        if span:
            # A snippet is code, not the prose that happens to sit after the
            # reference: "`internal/api/auth.go:246`; the adaptor call is at
            # `cmd/server/main.go:134`" made the words between the two
            # references into a "snippet" and reported a nonsense mismatch. The
            # test is punctuation a sentence does not have and code does --
            # `(`, `{`, `==`, `:=` -- deliberately not `:` or `;`, which prose
            # has in abundance.
            candidate = span.group(1).strip()
            if re.search(r"\(|\)|\{|\}|==|:=", candidate) and len(candidate) >= 6:
                snippet = candidate
        if resolved is None:
            yield claim("line-ref", doc, match.start(), FALSE,
                        "no file named %s in the tree" % path, text=match.group(0))
            continue
        with open(resolved, "r", encoding="utf-8", errors="replace") as fh:
            lines = fh.read().split("\n")
        if number > len(lines) or number == 0:
            yield claim("line-ref", doc, match.start(), FALSE,
                        "%s has %d lines, so line %d does not exist" % (os.path.relpath(resolved, shell.root), len(lines), number),
                        text=match.group(0))
            continue
        if snippet is None:
            yield claim("line-ref", doc, match.start(), CONFIRMED,
                        "%s has line %d" % (os.path.relpath(resolved, shell.root), number),
                        text=match.group(0))
            continue
        wanted = re.sub(r"\s+", " ", snippet).strip()
        target = re.sub(r"\s+", " ", lines[number - 1]).strip()
        if wanted in target:
            yield claim("line-ref", doc, match.start(), CONFIRMED,
                        "%s:%d contains %s" % (os.path.relpath(resolved, shell.root), number, snippet[:60]),
                        text=match.group(0))
            continue
        near = None
        for offset in range(1, 4):
            for candidate in (number - 1 - offset, number - 1 + offset):
                if 0 <= candidate < len(lines) and wanted in re.sub(r"\s+", " ", lines[candidate]):
                    near = candidate + 1
                    break
            if near:
                break
        if near:
            yield claim("line-ref", doc, match.start(), FALSE,
                        "%s: %s is on line %d, not %d" % (os.path.relpath(resolved, shell.root), snippet[:50], near, number),
                        text=match.group(0))
        else:
            yield claim("line-ref", doc, match.start(), FALSE,
                        "%s:%d does not contain %s" % (os.path.relpath(resolved, shell.root), number, snippet[:60]),
                        text=match.group(0))


ABSENCE_HINT = re.compile(r"no matches|returns? no match|is \*\*empty\*\*|returns? nothing|were removed", re.I)
# One alternation for both directions, plural-aware: "return no match" and
# "return no matches" are the same sentence shape, and an extractor that only
# knew the singular silently checked nothing on the form the document used.
ABSENCE_VERB = r"(?:returns? no match(?:es)?|is \*\*empty\*\*|returns? nothing|were removed)"
# The strings an absence claim is about are the ones *adjacent* to the hint,
# either just before it ("`X` returns no matches") or as the list after it
# ("return no matches: `X`, `Y`"). Anything else on the line is other prose --
# a file:line reference it also happens to mention is not a claimed absence,
# and treating it as one turns a correct document into a false alarm.
ABSENCE_BEFORE = re.compile(r"((?:`[^`]{2,80}`[\s,]*(?:and\s+)?)+)\s*" + ABSENCE_VERB, re.I)
ABSENCE_AFTER = re.compile(ABSENCE_VERB + r"\s*:?\s*((?:`[^`]{2,80}`[\s,]*(?:and\s+)?)+)", re.I)


def extract_absences(doc, shell, cache):
    """`git diff HEAD -- x and y` is empty, and stale anchors return no matches.

    Which target is searched is per claim: a sentence about the document ("these
    anchors return no matches") is checked against the document's *other* lines
    -- the line stating the claim always contains the string -- and one naming
    paths is checked against the tree. The evidence says which one answered.
    """
    target = shell.absence_target
    for line_number, line in enumerate(doc.lines, start=1):
        if not ABSENCE_HINT.search(line):
            continue
        position = doc.flat.find(line)
        diff = re.search(r"git diff HEAD -- ([^`]+)", line)
        if diff:
            paths = [p.strip().strip("`") for p in re.split(r"\s+and\s+|\s*,\s*", diff.group(1)) if p.strip()]
            argv, cwd, code, out = shell.command("git diff HEAD -- " + " ".join(paths))
            if argv is None or code is None:
                yield claim("absence", doc, position, UNVERIFIABLE,
                            "git diff not run: %s" % out, text=diff.group(0)[:100])
            elif out.strip() == "":
                yield claim("absence", doc, position, CONFIRMED,
                            "git diff HEAD -- %s is empty" % " ".join(paths), text=diff.group(0)[:100])
            else:
                changed = [l for l in out.splitlines() if l.startswith("+++ ")]
                yield claim("absence", doc, position, FALSE,
                            "git diff HEAD -- %s is not empty (%d changed file(s): %s)" % (
                                " ".join(paths), len(changed), ", ".join(c[6:] for c in changed[:3])),
                            text=diff.group(0)[:100])
            continue
        needles = []
        for pattern in (ABSENCE_BEFORE, ABSENCE_AFTER):
            for m in pattern.finditer(line):
                needles.extend(re.findall(r"`([^`]{2,80})`", m.group(1)))
        if not needles:
            continue
        others = "\n".join(l for n, l in enumerate(doc.lines, start=1) if n != line_number)
        for needle in needles:
            where = []
            if target in ("doc", "both") and needle in others:
                where.append("this document (another line)")
            found, hits = git_grep(shell, needle, cache)
            if target in ("tree", "both") and found:
                where.append("the tree (%s)" % hits[0][:60])
            if not where:
                yield claim("absence", doc, position, CONFIRMED,
                            "%r appears in neither the document's other lines nor the tree" % needle,
                            text=needle)
            else:
                yield claim("absence", doc, position, FALSE,
                            "%r still appears in %s" % (needle, " and ".join(where)),
                            text=needle)


def git_grep(shell, needle, cache):
    """Tracked worktree text search, via git so it is always available."""
    cache.setdefault("grep", {})
    if needle not in cache["grep"]:
        code, out = shell.git("grep", "-F", "-n", "--", needle)
        found = code == 0 and out.strip() != ""
        cache["grep"][needle] = (found, out.strip().splitlines()[:3])
    return cache["grep"][needle]


PR_NAME = r"(test|integration|postgres|quality|review|Playwright E2E(?: Tests)?|E2E)"
PR_STATE = r"(pass|passes|PASS|fail|fails|FAIL|pending|Pending|skipped|skipping)"


def extract_prs(doc, shell, cache):
    """`PR #359 @`deadbee`` and `test FAIL` / `integration pass`: gh's answer.

    This is the claim class that reads as current and is not: "its `test` job
    still fails" is true on the day it is written, and a stale line like that
    sends the next agent to fix something already fixed.
    """
    for m in re.finditer(r"(?:PR\s*)?#(\d{2,5})\b", doc.flat):
        number = m.group(1)
        if not re.search(r"PR|pull", doc.flat[max(0, m.start() - 60):m.start() + 6], re.I):
            continue
        if number in cache.setdefault("prs", {}):
            continue
        cache["prs"][number] = True
        line = doc.line_of(m.start())
        window = doc.lines[line - 1]
        if not shell.run_commands:
            yield claim("pr", doc, m.start(), UNVERIFIABLE,
                        "not queried (--run-commands not passed): gh pr view %s" % number,
                        text="#%s" % number)
            continue
        code, out = shell.gh("pr", "view", number, "--json", "state,headRefOid,url,mergeable")
        if code != 0 or not out.strip().startswith("{"):
            yield claim("pr", doc, m.start(), UNVERIFIABLE,
                        "gh pr view %s did not answer (offline or no gh): %s" % (number, (out or "").strip()[:80]),
                        text="#%s" % number)
            continue
        try:
            info = json.loads(out)
        except ValueError:
            yield claim("pr", doc, m.start(), UNVERIFIABLE, "gh pr view %s returned unparsable JSON" % number,
                        text="#%s" % number)
            continue
        sha = re.search(r"`" + SHA + r"`", window)
        detail = "gh pr view %s: state=%s head=%s mergeable=%s" % (
            number, info.get("state"), (info.get("headRefOid") or "")[:11], info.get("mergeable"))
        ok = True
        if sha and is_sha(sha.group(1)) and not (info.get("headRefOid") or "").startswith(sha.group(1)):
            ok = False
            detail += " (document claims %s)" % sha.group(1)
        stated = re.findall(PR_NAME + r"`?[^.\n]{0,40}?\*{0,2}" + PR_STATE, window)
        if stated:
            code2, out2 = shell.gh("pr", "checks", number)
            if code2 == 0 and out2.strip():
                observed = {}
                for row in out2.splitlines():
                    cols = row.split("\t")
                    if len(cols) >= 2:
                        observed[cols[0].strip().lower()] = cols[1].strip().lower()
                for job, said in stated:
                    key = job.strip().lower()
                    real = observed.get(key) or observed.get(key.replace(" tests", ""))
                    if real is None:
                        continue
                    said_norm = "pass" if said.lower().startswith("pass") else ("fail" if said.lower().startswith("fail") else said.lower())
                    real_norm = "pass" if real.startswith("pass") else ("fail" if real.startswith("fail") else real)
                    if said_norm != real_norm:
                        ok = False
                        detail += " | %s: document says %s, gh says %s" % (job, said, real)
                    else:
                        detail += " | %s: %s (as claimed)" % (job, real)
        yield claim("pr", doc, m.start(), CONFIRMED if ok else FALSE, detail, text="#%s" % number)


EXTRACTORS = [
    extract_revisions,
    extract_branches,
    extract_counts,
    extract_sizes,
    extract_versions,
    extract_commands,
    extract_line_refs,
    extract_absences,
    extract_prs,
]

CHECKS = [
    ("revision", "`deadbee`", "git rev-parse --verify <sha>^{commit}"),
    ("branch", "`branch` (head `deadbee`) / `branch` @`deadbee`", "git rev-parse <branch>, origin/<branch>"),
    ("count", "N tracked files modified / ~N backend/*.go / N codemap.md deletions", "git status --porcelain"),
    ("size", "`path` ... N,NNN bytes", "os.path.getsize (path, tracked basename, pruned walk)"),
    ("version", "`x/net v0.60.0`, `go 1.27.0`, module paths", "backend/go.mod"),
    ("command", "`go build ./...` -> exit 0 / exactly one `--- FAIL`", "re-run (needs --run-commands)"),
    ("line-ref", "`file.go:73` / `file.go` line 73 with a snippet", "that line of that file (same resolver)"),
    ("absence", "`x` returns no matches / `git diff HEAD -- a and b` is empty", "document and/or git grep"),
    ("pr", "PR #359 @`deadbee`, `test` FAIL / `integration` pass", "gh pr view / gh pr checks"),
]


def audit(doc, shell):
    """Every extractor, each isolated so one failure cannot hide the rest."""
    claims = []
    cache = {}
    shell.absence_target = getattr(shell, "absence_target", "both")
    for extractor in EXTRACTORS:
        try:
            claims.extend(extractor(doc, shell, cache))
        except Exception as exc:  # a broken extractor is reported, never silent
            claims.append({
                "kind": "internal",
                "line": 0,
                "text": extractor.__name__,
                "verdict": UNVERIFIABLE,
                "evidence": "extractor failed: %s: %s" % (type(exc).__name__, exc),
            })
    return claims


def render(doc, shell, claims):
    counts = {CONFIRMED: 0, FALSE: 0, UNVERIFIABLE: 0}
    for c in claims:
        counts[c["verdict"]] = counts.get(c["verdict"], 0) + 1
    lines = []
    lines.append("Handoff audit: %s" % doc.name)
    lines.append("repo: %s   commands: %s" % (
        shell.root, "re-run (--run-commands)" if shell.run_commands else "not re-run (git/rg checks only)"))
    lines.append("")
    if not claims:
        lines.append("No claims this tool understands. Nothing was checked -- that is not a pass.")
        return "\n".join(lines) + "\n", counts
    width = max(len(c["kind"]) for c in claims)
    lines.append("%-*s  %5s  %-12s  %s" % (width, "kind", "line", "verdict", "claim -> evidence"))
    for c in sorted(claims, key=lambda c: (c["line"], c["kind"])):
        lines.append("%-*s  %5s  %-12s  %s" % (
            width, c["kind"], c["line"] or "-", c["verdict"],
            "%s -> %s" % (c["text"].replace("\n", " "), c["evidence"])))
    lines.append("")
    lines.append("%d check(s): %d confirmed, %d false, %d unverifiable" % (
        len(claims), counts[CONFIRMED], counts[FALSE], counts[UNVERIFIABLE]))
    false_ones = [c for c in claims if c["verdict"] == FALSE]
    if false_ones:
        lines.append("")
        lines.append("Claims the next agent must not trust (FALSE):")
        for c in false_ones:
            lines.append("  - line %s: %s" % (c["line"] or "-", c["text"][:100]))
        lines.append("")
        lines.append("Fix each one in the document, or state plainly that it is stale and why.")
    if counts[UNVERIFIABLE]:
        lines.append("")
        lines.append("Unverifiable here (not evidence either way): pass --run-commands for "
                     "command/PR checks, or accept them as unverified.")
    return "\n".join(lines) + "\n", counts


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Re-derive a handoff document's factual claims from the tree.")
    parser.add_argument("document", help="path to the handoff markdown")
    parser.add_argument("--root", default=REPO_ROOT, help="tree to audit against (default: this repo)")
    parser.add_argument("--run-commands", action="store_true",
                        help="also re-run the documents's go/git/gh commands (slow, needs network for gh)")
    parser.add_argument("--strict", action="store_true",
                        help="treat UNVERIFIABLE as a failure too")
    parser.add_argument("--json", action="store_true", help="machine-readable output")
    parser.add_argument("--absence-target", choices=("doc", "tree", "both"), default="both",
                        help="where 'returns no matches' claims are searched")
    args = parser.parse_args(argv)

    try:
        with open(args.document, "r", encoding="utf-8", errors="replace") as fh:
            text = fh.read()
    except OSError as exc:
        print("handoff_audit: cannot read %s: %s" % (args.document, exc), file=sys.stderr)
        return 2

    shell = Shell(args.root, run_commands=args.run_commands)
    shell.absence_target = args.absence_target
    doc = Doc(text, name=args.document)
    claims = audit(doc, shell)

    if args.json:
        print(json.dumps({
            "document": args.document,
            "root": args.root,
            "claims": claims,
        }, indent=2))
        counts = {v: len([c for c in claims if c["verdict"] == v])
                  for v in (CONFIRMED, FALSE, UNVERIFIABLE)}
    else:
        report, counts = render(doc, shell, claims)
        sys.stdout.write(report)

    if not claims:
        return 2
    if counts[FALSE]:
        return 1
    if args.strict and counts[UNVERIFIABLE]:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Mutation-check scripts/linear.py against scripts/test_linear.py.

A green suite proves nothing until you see it go red for the right reason. Each
mutation below breaks one real defect class this CLI exists to prevent, and the
harness requires the NAMED test to fail. Restore is from a snapshot, never
`git checkout --`, so uncommitted work elsewhere in the tree is untouched.

Usage:  python scripts/linear_mutation_check.py
Exit 0 only if every mutation is CAUGHT and every control still passes.
"""

import io
import os
import re
import shutil
import subprocess
import sys
import tempfile

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))
CLI = os.path.join(HERE, "linear.py")
TESTS = os.path.join(HERE, "test_linear.py")

# (label, old, new, test that must fail)
#
# Every replacement keeps the file syntactically valid: a mutation that does not
# compile fails every case INCLUDING the controls, which reads as 100% caught
# while measuring nothing.
MUTATIONS = [
    (
        "M1 write description instead of content",
        'api.gql(\n        """mutation($id:String!,$c:String!){ projectUpdate(id:$id, input:{content:$c}){',
        'api.gql(\n        """mutation($id:String!,$c:String!){ projectUpdate(id:$id, input:{description:$c}){',
        "test_mutation_sends_content_and_never_description",
    ),
    (
        "M2 drop the no-op guard (always write)",
        "    if not show_diff(current, proposed, \"project-body\"):\n        return 0",
        "    show_diff(current, proposed, \"project-body\")",
        "test_identical_body_writes_nothing",
    ),
    (
        "M3 let --dry-run write anyway",
        "    if args.dry_run:\n        sys.stderr.write(\"linear: --dry-run, not writing\\n\")\n        return 0",
        "    if args.dry_run:\n        sys.stderr.write(\"linear: --dry-run, not writing\\n\")",
        "test_dry_run_never_writes",
    ),
    (
        # Strike the USE site, not the assignment: injecting `match` before the
        # lookup runs is inert because the lookup overwrites it.
        "M4 send state NAME where a UUID is required",
        '        {"id": args.issue, "s": match[0]["id"]},',
        '        {"id": args.issue, "s": args.state},',
        "test_resolves_state_by_name_and_sends_uuid",
    ),
    (
        "M5 filter issues via Query.issues args instead of filter:",
        '        "query(%s){ issues(first:%d%s, orderBy: updatedAt){ "',
        '        "query(%s){ issues(first:%d, orderBy: updatedAt, project: {id: {eq: $pid}}){ "',
        "test_filter_goes_inside_filter_argument",
    ),
    (
        "M6 declare $pid as String (validation error at Linear)",
        '        decls.append("$pid:ID")',
        '        decls.append("$pid:String")',
        "test_filter_goes_inside_filter_argument",
    ),
    (
        "M7 use teamId (singular) instead of teamIds array",
        '        inp += ", teamIds: $teams"',
        '        inp += ", teamId: $team"',
        "test_uses_team_ids_array_not_team_id",
    ),
    (
        "M8 treat a populated errors array as success",
        '            errs = body.get("errors")\n            if errs:',
        '            errs = body.get("errors")\n            if False:',
        "test_partial_success_errors_array_is_fatal",
    ),
    (
        "M9 resolve project via an identifier filter that does not exist",
        '        """query($id:String!){ project(id:$id){ id identifier name\n'
        '             teams{ nodes{ id key } } } }""",',
        '        """query($id:String!){ projects(first:5, filter:{identifier:{eq:$id}})'
        '{ nodes{ id identifier name teams{ nodes{ id key } } } } }""",',
        "test_direct_identifier_lookup_is_one_call",
    ),
]

# Controls: semantically neutral rewrites that MUST still pass. A stale anchor
# matches zero times and silently reads as a caught mutation, so controls are
# what prove the harness is actually looking at the right code.
CONTROLS = [
    (
        "C1 rename a local variable",
        "    api.throttle()\n    # `content` only.",
        "    api.throttle()\n    # content only.",
        None,
    ),
    (
        "C2 reword a comment",
        "    # Explicit `first` keeps complexity low: default pagination is 50 and the\n"
        "    # cost multiplies across child fields.",
        "    # Explicit `first` keeps complexity low: the default is 50 and cost\n"
        "    # multiplies across child fields.",
        None,
    ),
    (
        "C3 reorder an independent assignment",
        "        conds.append(\"project:{id:{eq:$pid}}\")",
        "        conds = [\"project:{id:{eq:$pid}}\"] + conds",
        None,
    ),
]


def run_tests():
    proc = subprocess.run(
        [sys.executable, TESTS],
        capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    return proc.returncode, proc.stdout + proc.stderr


def failing_tests(output):
    """Names of failed tests.

    `--- FAIL: TestName (0.00s)` splits to ['---', 'FAIL:', 'TestName', ...],
    so the name is index 2. Index 1 is the literal 'FAIL:', which matches
    nothing and makes every mutation score MISSED.
    """
    names = []
    for line in output.splitlines():
        if line.startswith("--- FAIL:") or line.startswith("FAIL:"):
            parts = line.split()
            if len(parts) > 2:
                names.append(parts[2])
    for line in output.splitlines():
        m = re.match(r"^(?:ERROR|FAIL):\s+(\w+)", line.strip())
        if m:
            names.append(m.group(1))
    return names


def dominant_eol(text):
    """CRLF wins only if it actually dominates; otherwise LF."""
    crlf = text.count("\r\n")
    lf = text.count("\n") - crlf
    return "\r\n" if crlf > lf else "\n"


def normalise(text):
    """Compare on LF only, so an anchor matches in either ending."""
    return text.replace("\r\n", "\n")


def write_cli(content, template):
    """Write content using the template file own dominant ending."""
    io.open(CLI, "w", encoding="utf-8", newline="").write(
        content.replace("\n", dominant_eol(template))
    )


def apply_mutation(text, old, new, label):
    haystack = normalise(text)
    needle = normalise(old)
    if needle not in haystack:
        return None, "ANCHOR STALE"
    if haystack.count(needle) != 1:
        return None, "anchor matched %d times" % haystack.count(needle)
    mutated = haystack.replace(needle, normalise(new))
    return mutated, None


def main():
    original = io.open(CLI, encoding="utf-8", newline="").read()
    snap = tempfile.NamedTemporaryFile("w", suffix=".py", delete=False,
                                       encoding="utf-8", newline="")
    snap.write(original)
    snap.close()

    failures = []
    try:
        # Baseline must be green before any mutation means anything.
        rc, out = run_tests()
        print("baseline: %s" % ("PASS" if rc == 0 else "FAIL"))
        if rc != 0:
            print(out[-2000:])
            return 1

        print("\n--- controls (must PASS) ---")
        for label, old, new, _ in CONTROLS:
            mutated, err = apply_mutation(original, old, new, label)
            if err:
                print("  %-42s STALE ANCHOR (%s) -- harness is broken" % (label, err))
                failures.append(label + " stale anchor")
                continue
            write_cli(mutated, original)
            rc, out = run_tests()
            ok = rc == 0
            print("  %-42s %s" % (label, "PASS" if ok else "UNEXPECTEDLY FAILED"))
            if not ok:
                failures.append(label)
                print(out[-1500:])
        write_cli(original, original)

        print("\n--- mutations (must be CAUGHT) ---")
        for label, old, new, test_name in MUTATIONS:
            mutated, err = apply_mutation(original, old, new, label)
            if err:
                print("  %-42s STALE ANCHOR (%s) -- VOID" % (label, err))
                failures.append(label + " stale anchor")
                continue
            write_cli(mutated, original)

            # A mutation that will not compile is VOID, not caught.
            chk = subprocess.run([sys.executable, "-m", "py_compile", CLI],
                                 capture_output=True, text=True)
            if chk.returncode != 0:
                print("  %-42s VOID (does not compile)" % label)
                failures.append(label + " void")
                continue

            rc, out = run_tests()
            failed = failing_tests(out)
            caught = rc != 0
            by_name = test_name in failed
            if caught and by_name:
                print("  %-42s CAUGHT by %s" % (label, test_name))
            elif caught:
                print("  %-42s CAUGHT but not by %s (failed: %s)"
                      % (label, test_name, ", ".join(failed) or "none"))
                failures.append(label + " wrong test")
            else:
                print("  %-42s MISSED (suite stayed green)" % label)
                failures.append(label + " missed")
        write_cli(original, original)
    finally:
        shutil.copyfile(snap.name, CLI)
        os.unlink(snap.name)

    # Restore must be byte-exact, verified from the snapshot.
    rc, out = run_tests()
    print("\nrestored: %s" % ("PASS" if rc == 0 else "FAIL"))
    if rc != 0:
        print(out[-2000:])
        failures.append("restore left the tree broken")
    if normalise(io.open(CLI, encoding="utf-8", newline="").read()) != normalise(original):
        print("RESTORE MISMATCH -- file differs from snapshot")
        failures.append("restore mismatch")

    print("\n=== %d/%d caught, %d/%d controls green ==="
          % (len(MUTATIONS) - len([f for f in failures if "missed" in f or "wrong" in f or "void" in f or "stale" in f]),
             len(MUTATIONS),
             len(CONTROLS) - len([f for f in failures if f.startswith("C")]),
             len(CONTROLS)))
    if failures:
        print("PROBLEMS: %s" % "; ".join(failures))
        return 1
    print("OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
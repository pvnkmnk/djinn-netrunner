"""Mutation check for the e2e credential guard.

Every mutation must be CAUGHT and every control must PASS. Mutations restore from
an in-process snapshot, never `git checkout --`, which would wipe unrelated
uncommitted work. A build failure is VOID, not caught, and is reported as such.

Run from the repo root:  python scripts/e2e_credentials_mutation_check.py
"""

import io
import os
import subprocess
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

REPO = os.getcwd()
BACKEND = os.path.join(REPO, "backend")

FIXTURE = os.path.join(REPO, "e2e", "fixtures", "auth.fixture.ts")
SEED = os.path.join(REPO, "e2e", "setup-test-db.sh")
SUBSONIC = os.path.join(REPO, "e2e", "tests", "subsonic.spec.ts")

GUARD_TESTS = (
    "TestE2EFixtureCredentialsSatisfyTheRegistrationPolicy",
    "TestE2ESeededAdminHashMatchesTheFixturePassword",
    "TestOnlyTheFixtureDeclaresTheSharedCredentials",
)

# The hash the seed carried before this fix: bcrypt("admin123"). Reinstating it
# is the exact "seed and fixture disagree" failure the guard exists to catch.
OLD_HASH = "\\$2a\\$10\\$DAbZ8zqRgGGkdgDfkV0FduOIxRBfrrqjV7q4GYC/gf1z/Wtkg672m"
NEW_HASH = "\\$2a\\$10\\$8af93pHkmaF6skl8bWzE2euXTc.njLm4YCHtrEZqNs55Qyi7BMyJ2"

snapshots = {}


def snapshot(path):
    if path not in snapshots:
        with io.open(path, "rb") as fh:
            snapshots[path] = fh.read()


def restore_all():
    for path, data in snapshots.items():
        with io.open(path, "wb") as fh:
            fh.write(data)


def patch(path, old, new, count=1):
    snapshot(path)
    with io.open(path, "rb") as fh:
        text = fh.read().decode("utf-8")
    newline = "\r\n" if "\r\n" in text else "\n"
    old = old.replace("\r\n", "\n").replace("\n", newline)
    new = new.replace("\r\n", "\n").replace("\n", newline)
    if text.count(old) != count:
        raise SystemExit("ANCHOR STALE (%d != %d) in %s:\n%s" % (text.count(old), count, path, old[:200]))
    with io.open(path, "wb") as fh:
        fh.write(text.replace(old, new).encode("utf-8"))


def run_guard():
    env = dict(os.environ)
    env["PORT"] = ""
    proc = subprocess.run(
        ["go", "test", "./internal/api/", "-run", "|".join(GUARD_TESTS), "-v", "-count=1"],
        cwd=BACKEND, env=env, capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    out = proc.stdout + proc.stderr
    # `--- FAIL: TestName (0.00s)` splits into ['---', 'FAIL:', 'TestName', '(0.00s)'],
    # so the name is index 2. Index 1 is the literal 'FAIL:', which silently made
    # every mutation un-catchable and reported the whole suite as MISSED.
    # Subtests are indented, so only top-level names are collected - that is what
    # the targets below name.
    failed = sorted(set(
        line[len("--- FAIL: "):].split()[0]
        for line in out.splitlines() if line.startswith("--- FAIL: ")
    ))
    # Scoped to the compiler's own wording. A bare "cannot find" also matches an
    # ordinary assertion message and would report a real catch as VOID.
    void = (
        "build failed" in out
        or "cannot find package" in out
        or "no required module provides" in out
        or "undefined:" in out
    )
    return proc.returncode == 0, failed, void


# ---------------------------------------------------------------- mutations

def m1_short_password():
    # Exactly the regression this slice fixes: `testpass123` is 11 characters,
    # one under the floor. The first version of this mutation used a 12-character
    # replacement, which still cleared the floor - the guard was right to pass it
    # and the mutation was measuring nothing.
    patch(FIXTURE, "password: 'e2eTestPass1234'", "password: 'testpass123'")


def m2_stale_seed_hash():
    # The seed and the fixture disagree - the admin seat cannot log in.
    patch(SEED, NEW_HASH, OLD_HASH)


def m3_duplicated_credential():
    # The shape both stale specs had: the credential restated by hand.
    patch(
        SUBSONIC,
        "import { test, TEST_USER } from '../fixtures/auth.fixture';",
        "import { test } from '../fixtures/auth.fixture';\n\nconst TEST_USER = { email: 'e2e-test@netrunner.dev', password: 'e2eTestPass1234' };",
    )


# ----------------------------------------------------------------- controls

def c1_benign_spec_title():
    patch(SUBSONIC, "Ping succeeds with valid auth", "Ping succeeds with valid credentials")


def c2_benign_fixture_comment():
    patch(FIXTURE, "const REPO_ROOT = path.resolve(__dirname, '..');",
          "// Repository root for the admin-promotion fallback below.\nconst REPO_ROOT = path.resolve(__dirname, '..');")


def c3_benign_seed_comment():
    patch(SEED, "echo \"=== Seeding admin user ===\"", "echo \"=== Seeding the e2e admin user ===\"")


MUTATIONS = [
    ("M1", "fixture password one under the floor", m1_short_password, GUARD_TESTS[0]),
    ("M2", "seeded hash no longer matches the fixture", m2_stale_seed_hash, GUARD_TESTS[1]),
    ("M3", "a spec restates the shared credential", m3_duplicated_credential, GUARD_TESTS[2]),
]

CONTROLS = [
    ("C1", "benign spec title change", c1_benign_spec_title),
    ("C2", "benign fixture comment added", c2_benign_fixture_comment),
    ("C3", "benign seed echo change", c3_benign_seed_comment),
]

results = []


def record(kind, ident, desc, expect_fail, target):
    ok, failed, void = run_guard()
    if void:
        results.append((kind, ident, desc, "VOID", failed))
    elif expect_fail:
        caught = (not ok) and any(t == target for t in failed)
        results.append((kind, ident, desc, "CAUGHT" if caught else "MISSED", failed))
    else:
        passed = ok and not failed
        results.append((kind, ident, desc, "PASS" if passed else "BROKE", failed))


try:
    print("baseline ...", flush=True)
    ok, failed, void = run_guard()
    baseline_green = ok and not failed and not void
    print("  baseline:", "GREEN" if baseline_green else "NOT GREEN", failed, flush=True)

    for ident, desc, fn, target in MUTATIONS:
        restore_all()
        fn()
        record("MUT", ident, desc, True, target)
        print("  %s %s -> %s" % (ident, desc, results[-1][3]), flush=True)

    for ident, desc, fn in CONTROLS:
        restore_all()
        fn()
        record("CTL", ident, desc, False, None)
        print("  %s %s -> %s" % (ident, desc, results[-1][3]), flush=True)

    restore_all()
    ok, failed, void = run_guard()
    restored_green = ok and not failed and not void
    print("restored control:", "GREEN" if restored_green else "NOT GREEN", failed, flush=True)
finally:
    restore_all()

print()
print("%-4s %-46s %-7s %s" % ("id", "mutation", "result", "failing tests"))
for kind, ident, desc, verdict, failed in results:
    print("%-4s %-46s %-7s %s" % (ident, desc[:46], verdict, ",".join(failed) or "-"))

caught = sum(1 for r in results if r[0] == "MUT" and r[3] == "CAUGHT")
muts = sum(1 for r in results if r[0] == "MUT")
controls_ok = sum(1 for r in results if r[0] == "CTL" and r[3] == "PASS")
ctls = sum(1 for r in results if r[0] == "CTL")

print()
print("mutations caught %d/%d | controls green %d/%d | baseline %s | restored %s"
      % (caught, muts, controls_ok, ctls,
         "GREEN" if baseline_green else "RED", "GREEN" if restored_green else "RED"))
sys.exit(0 if (caught == muts and controls_ok == ctls and baseline_green and restored_green) else 1)
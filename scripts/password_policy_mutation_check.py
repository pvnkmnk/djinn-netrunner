#!/usr/bin/env python3
"""Mutation-check the shared password policy (DJI-559 / DJI-600).

This is the GO-LEVEL harness. It mutates source and runs `go test`; nothing is
built into an image and no container is involved. The browser-level proof for
the same policy -- the Playwright spec against a REBUILT container -- is
deliberately NOT here: it needs docker and a running stack, so it belongs in a
different place with a different cost.

What it proves. `config.ValidatePassword` is the single source of truth for the
password floor and bcrypt's byte ceiling, and three routes call it:
registration, admin create-user, admin reset-password. A green suite is only
evidence if it has been seen going red for the right reason, so each mutation
below breaks one real defect class and the harness requires the suite to FAIL.

Four of the eight mutations remove a CALL SITE rather than breaking the helper.
That is the failure mode a shared helper invites -- the policy exists, is
correct, and nobody asks it -- and it is invisible to any test of the helper
alone.

Integrity rules, each of which exists because ignoring it produced a false
result during the work this harness was written from:

  * VOID is not caught. `go test` runs a vet subset, so a mutation that merely
    fails the compiler or vet fails every case INCLUDING the control, and a
    harness that scores that as a catch reports a perfect run while proving
    nothing. A build failure emits no `--- FAIL:` lines at all; that absence is
    the signal, not a keyword search over output that can legitimately contain
    compiler words inside assertion messages.
  * A green control must pass BEFORE any mutant runs. If it does not, the run
    is void and stops there rather than reporting its mutants.
  * The restore must be proven byte-identical, before the final control runs.
    A harness that mutates and restores in a `finally` will happily run the
    control against a still-mutant tree, and the control passes for the wrong
    reason.
  * Every anchor and fixture is measured before use. A "long passphrase" that
    is 57 bytes is under a 72-byte ceiling, and a boundary fixture that does not
    assert its own size proves nothing.

Anchors are re-derived from the code as it stands and verified at startup. If
the policy helper moves or a call site is added or removed, this harness fails
immediately with a message saying which anchor is stale, rather than silently
mutating nothing and reporting a clean run.

Usage:  python scripts/password_policy_mutation_check.py
Exit 0 only if every mutation is CAUGHT and the controls pass.
"""

import io
import os
import re
import subprocess
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
BACKEND = os.path.join(ROOT, "backend")

HELPER = os.path.join(BACKEND, "internal", "config", "password_policy.go")
AUTH = os.path.join(BACKEND, "internal", "api", "auth.go")
ADMIN = os.path.join(BACKEND, "internal", "api", "admin_handler.go")
HELPER_TEST = os.path.join(BACKEND, "internal", "config", "password_policy_test.go")
ADMIN_TEST = os.path.join(BACKEND, "internal", "api", "admin_password_policy_test.go")

# The packages and tests that must notice. Both policy packages, because the
# helper can be broken without any route changing, and a route can stop asking
# without the helper changing.
PKGS = ["./internal/config", "./internal/api"]
TEST_FILTER = "Password|Register|Ceiling|Admin"

BS = chr(92)
TAB = chr(9)
NL_CHAR = chr(10)


class StaleAnchor(SystemExit):
    """The code moved. Better a loud stop than a silent no-op that reports green."""


def read(path):
    with io.open(path, encoding="utf-8", newline="") as fh:
        return fh.read()


def write(path, text):
    with io.open(path, "w", encoding="utf-8", newline="") as fh:
        fh.write(text)


def go_test():
    """Run the policy tests. PORT is cleared: this environment exports PORT=0,
    which breaks internal/config's own defaults test."""
    env = dict(os.environ, PORT="")
    return subprocess.run(
        ["go", "test"] + PKGS + ["-run", TEST_FILTER, "-count=1"],
        cwd=BACKEND, capture_output=True, text=True,
        encoding="utf-8", errors="replace", env=env, timeout=1800,
    )


BUILD_MARKERS = (
    "build failed", "[build failed]", "imported and not used", "syntax error",
    "cannot use", "undefined:", "declared and not used",
    "too many arguments", "not enough arguments",
)


def classify(proc):
    """PASS / FAIL / VOID, and the failing test names.

    VOID means the mutant did not BUILD. Deciding that on the absence of
    `--- FAIL:` lines rather than on a keyword search is the whole point: a
    build failure produces none, while a real assertion failure can easily
    quote a compiler message back at you.
    """
    out = proc.stdout + proc.stderr
    failed = sorted({m.group(1) for m in re.finditer(r"^--- FAIL: (\S+)", out, re.M)})
    build_broke = any(m in out for m in BUILD_MARKERS)
    if build_broke and not failed:
        status = "VOID"
    elif proc.returncode == 0:
        status = "PASS"
    else:
        status = "FAIL"
    return status, failed, out


def show(status, failed, limit=6):
    print("    %-5s failing: %s" % (status, ", ".join(failed[:limit]) or "none"))
    if len(failed) > limit:
        print("           ... and %d more" % (len(failed) - limit))


# --------------------------------------------------------------------------
# Anchors, verified against the code as it stands.
# --------------------------------------------------------------------------

H_SRC = read(HELPER)
A_SRC = read(AUTH)
D_SRC = read(ADMIN)
# A real CRLF is CR + LF, two characters. Building it from a backslash
# yields a three-character string that matches nothing -- which is exactly
# what this file's own startup guard caught on its first run.
CR = chr(13)
# Detect by looking for CRLF itself. Testing for a bare LF is wrong in both
# directions: every CRLF file also contains a line feed, so an LF checkout
# is misread as CRLF and every anchor in it then matches nothing.
NL = CR + NL_CHAR if (CR + NL_CHAR) in H_SRC else NL_CHAR


def crlf(s):
    """Normalise a literal to the file's own line ending. This harness runs on
    CI (LF) and on a Windows checkout (CRLF); a bare newline in an anchor
    matches one and not the other, which reads as 'mutation had no effect'."""
    return s.replace(BS + "n", NL)


def require(condition, message):
    if not condition:
        sys.stderr.write("STALE ANCHOR: %s\n" % message)
        sys.stderr.write(
            "This harness re-derives its anchors from the code. The password "
            "policy helper has moved, or a call site was added or removed.\n"
            "Re-derive them and update the header before trusting a green run.\n")
        raise StaleAnchor(2)


# The three-line call site every route shares.
CALL_SITE = crlf(
    TAB + "if v := config.ValidatePassword(payload.Password, h.minPasswordLength); v != nil {"
    + NL + TAB * 2 + "return c.Status(400).JSON(v.JSON())"
    + NL + TAB + "}")

require(A_SRC.count(CALL_SITE) == 1,
        "expected exactly 1 config.ValidatePassword call site in auth.go, found %d"
        % A_SRC.count(CALL_SITE))
require(D_SRC.count(CALL_SITE) == 2,
        "expected exactly 2 config.ValidatePassword call sites in admin_handler.go, found %d"
        % D_SRC.count(CALL_SITE))
require("if size := len([]byte(password)); size > BcryptMaxPasswordBytes {" in H_SRC,
        "the byte-ceiling check is no longer shaped as expected in password_policy.go")
require("if length := utf8.RuneCountInString(password); length < minLength {" in H_SRC,
        "the character-floor check is no longer shaped as expected in password_policy.go")
require("minLength = DefaultMinPasswordLength" in H_SRC,
        "the unconfigured-policy default is gone from password_policy.go")

CEILING_MESSAGE = crlf(
    '"password must be at most %d bytes; this one is %d bytes. This limit counts '
    'bytes, not characters, so a passphrase using accented or non-Latin characters '
    'can reach it in fewer characters"')
require(CEILING_MESSAGE in H_SRC,
        "the ceiling message text is no longer shaped as expected in password_policy.go")

# Fixture self-measurement. A passphrase that is UNDER the ceiling turns a
# "proves a refusal" case into a "proves acceptance" case, which is how this
# harness's own upstream fixture silently measured nothing.
PASSPHRASE = ("correct horse battery staple vanilla harbor candle drawer "
              "lonely summit kitten")
CEILING_BYTES = 72
require(len(PASSPHRASE.encode("utf-8")) > CEILING_BYTES,
        "the passphrase fixture is only %d bytes, UNDER the %d-byte ceiling"
        % (len(PASSPHRASE.encode("utf-8")), CEILING_BYTES))
for fixture, label in ((HELPER_TEST, "password_policy_test.go"), (ADMIN_TEST, "admin_password_policy_test.go")):
    body = read(fixture)
    require(PASSPHRASE in body,
            "the passphrase fixture is no longer in %s; the two files must be "
            "mutated and asserted together" % label)
# The at-the-ceiling multi-byte case, in both test files. It is the only
# row that proves a boundary rather than a refusal: 36 e-acutes are 72 bytes.
for _f, _label in ((HELPER_TEST, "password_policy_test.go"),
                  (ADMIN_TEST, "admin_password_policy_test.go")):
    require("ceiling/2" in read(_f),
            "the multi-byte at-the-ceiling fixture is gone from %s" % _label)


# --------------------------------------------------------------------------
# Mutations
# --------------------------------------------------------------------------

def sub(text, old, new, nth=1):
    """Replace the nth occurrence.

    Occurrence nth is the nth SEPARATOR between split parts, so joining
    parts[:nth] with parts[nth:] drops exactly one and leaves the rest. An
    earlier version joined the halves separately and silently removed every
    occurrence at once, which made two "drop a call site" mutants identical and
    produced a false proof for one of them.
    """
    o, n = crlf(old), crlf(new)
    require(o in text, "anchor missing: %r" % o[:70])
    parts = text.split(o)
    require(len(parts) - 1 >= nth,
            "only %d occurrence(s) of %r, wanted the %d" % (len(parts) - 1, o[:50], nth))
    return o.join(parts[:nth]) + n + o.join(parts[nth:])


def m_ceiling_counts_runes(h, a, d):
    """The floor/ceiling mix-up: a rune-counted ceiling lets 40 e-acutes
    (80 bytes) through, and bcrypt still refuses them."""
    return sub(h, "len([]byte(password))", "len([]rune(password))"), a, d


def m_ceiling_off_by_one(h, a, d):
    return sub(h, "size > BcryptMaxPasswordBytes", "size > BcryptMaxPasswordBytes+1"), a, d


def m_ceiling_unreachable(h, a, d):
    return sub(h, "size > BcryptMaxPasswordBytes", "size > 1<<20"), a, d


def m_message_says_only_too_long(h, a, d):
    """A refusal that names no unit and no number is what a user cannot act on."""
    return sub(h, CEILING_MESSAGE, '"password too long"'), a, d


def m_default_floor_zero(h, a, d):
    """A handler with no configured policy would then enforce no floor at all --
    which is how the two admin routes behaved before DJI-600."""
    return sub(h, "minLength = DefaultMinPasswordLength", "minLength = 0"), a, d


def m_drop_create_user_call(h, a, d):
    """nth=1 is CreateUser; nth=2 below is ResetPassword. The two call sites are
    byte-identical, so the ordinal is the only thing that tells them apart."""
    return h, a, sub(d, CALL_SITE, "", 1)


def m_drop_reset_call(h, a, d):
    return h, a, sub(d, CALL_SITE, "", 2)


def m_drop_registration_call(h, a, d):
    return h, sub(a, CALL_SITE, ""), d


MUTATIONS = [
    ("M1 ceiling counts runes instead of bytes", m_ceiling_counts_runes),
    ("M2 ceiling off by one (73 bytes accepted)", m_ceiling_off_by_one),
    ("M3 ceiling out of reach (guard never fires)", m_ceiling_unreachable),
    ("M4 ceiling message says only 'too long'", m_message_says_only_too_long),
    ("M5 an unconfigured handler enforces no floor", m_default_floor_zero),
    ("M6 CreateUser stops calling the helper", m_drop_create_user_call),
    ("M7 ResetPassword stops calling the helper", m_drop_reset_call),
    ("M8 Register stops calling the helper", m_drop_registration_call),
]

# Every mutant must be a DIFFERENT file state. Two identical states mean the
# mutations are not testing what they claim to be testing.
STATES = {}
for label, mutate in MUTATIONS:
    result = mutate(H_SRC, A_SRC, D_SRC)
    require(all(isinstance(part, str) for part in result),
            "%s did not produce text" % label)
    require(tuple(result) != (H_SRC, A_SRC, D_SRC),
            "%s changed nothing in any of the three files" % label)
    key = tuple(result)
    require(key not in STATES,
            "%s produces a file state another mutation already produced -- "
            "they are one mutation, and one of them is not being proved" % label)
    STATES[key] = label


def apply(state):
    for path, text in zip((HELPER, AUTH, ADMIN), state):
        write(path, text)


def restore_ok():
    return (read(HELPER) == H_SRC and read(AUTH) == A_SRC and read(ADMIN) == D_SRC)


def main():
    print("Go-level mutation proof: the shared password policy")
    print("  helper   %s" % os.path.relpath(HELPER, ROOT))
    print("  callers  auth.go x1, admin_handler.go x2  (verified at startup)")
    print("  control  go test %s -run %s" % (" ".join(PKGS), TEST_FILTER))
    print()

    print("CONTROL (unmutated, must pass):")
    status, failed, _ = classify(go_test())
    show(status, failed)
    if status != "PASS":
        sys.stderr.write("ABORT: the control did not pass, so every result below "
                         "would be meaningless. The whole run is VOID.\n")
        return 2

    results = []
    try:
        for label, mutate in MUTATIONS:
            print("\n%s" % label)
            apply(mutate(H_SRC, A_SRC, D_SRC))
            status, failed, _ = classify(go_test())
            show(status, failed)
            results.append((label, status, failed))
    finally:
        apply((H_SRC, A_SRC, D_SRC))

    if not restore_ok():
        sys.stderr.write("RESTORE FAILED: a source file is not byte-identical to "
                         "its pre-mutation content. Nothing after this point is "
                         "trustworthy -- run `git status` before anything else.\n")
        return 2
    print("\nRestore proven byte-identical for all three files.")

    caught = sum(1 for _, s, _ in results if s == "FAIL")
    total = len(results)

    # The final control runs AFTER the restore, so it cannot pass against a
    # still-mutant tree.
    print("\nCONTROL after restore (must pass):")
    status, failed, _ = classify(go_test())
    show(status, failed)
    if status != "PASS":
        sys.stderr.write("ABORT: the post-restore control failed. The mutation "
                         "results above are unusable.\n")
        return 2

    print("\n%-6s %s" % ("CONTROL", "green (before and after)"))
    for label, status, failed in results:
        mark = {"FAIL": "caught", "VOID": "VOID (unusable)",
                "PASS": "MISSED"}[status]
        print("%-6s %-48s [%s]" % (status, label, mark))

    print("\n%d/%d mutations caught; both controls green" % (caught, total))
    if caught != total:
        sys.stderr.write("A MISSED means the suite does not actually cover that "
                         "behaviour, whatever the exit code says.\n")
    return 0 if caught == total else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except StaleAnchor:
        sys.exit(2)
    except KeyboardInterrupt:
        sys.exit(130)
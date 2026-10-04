#!/usr/bin/env python3
"""Mutation-check the promise that monitoring an artist scans it (DJI-588).

This is the GO-LEVEL harness, and the SIBLING of
scripts/password_policy_mutation_check.py. It carries the same four rules,
because the same four mistakes make a mutation report look better than it is:

  * VOID is not caught. `go test` runs a vet subset, so a mutation that merely
    fails the compiler or vet fails every case INCLUDING the control, and a
    harness that scores that as a catch reports a perfect run while proving
    nothing. A build failure emits no `--- FAIL:` lines at all; that absence is
    the signal, not a keyword search over output that can legitimately contain
    compiler words inside assertion messages.
  * A green control must pass BEFORE any mutant runs, and again AFTER the
    restore. The second one cannot pass against a still-mutant tree, which is
    the only way to know the restore worked.
  * The restore must be proven byte-identical, not assumed.
  * Every anchor is verified against the code as it stands. A stale anchor that
    silently matches nothing would score every mutation as a miss -- or, worse,
    as a pass.

Scope note. The BROWSER-level proofs for this behaviour are
e2e/tests/artist-scan.spec.ts and e2e/tests/artist-picker.spec.ts, which need the
templates baked into a rebuilt container. They are deliberately not here. What IS
here is provable without a running stack: the template mutations are caught at the
Go level because the guard tests read the templates and the CSP header from disk.

Two promises, one harness. The second is that the CSP the app SERVES still admits
what the templates need. The Add Artist picker used to carry its pick in an htmx
values expression, which htmx compiles with eval; script-src 'self' forbids eval,
so the click threw, issued no request, and the artist was never added. The fix was
on the template side -- plain values collected by hx-include -- and M9/M10 keep it
there: M9 puts the expression back, M10 is the WRONG fix applied to the header.
Neither is allowed to pass silently.

What it proves. `ArtistTrackingService.QueueArtistScan` is the single owner of
"a monitored artist's scan reaches the queue". AddMonitoredArtist calls it
inside the transaction that writes the row, and POST /api/artists/:id/sync calls
it for the operator who wants a fresh look. Two callers, one owner -- the shape
DJI-600 applied to the password policy. Each mutation below breaks one real
defect class of that promise and the harness requires the suite to FAIL.
"""
import io
import os
import re
import subprocess
import sys

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
BACKEND = os.path.join(ROOT, "backend")

SVC = os.path.join(BACKEND, "internal", "services", "artist_tracking_service.go")
TPL = os.path.join(ROOT, "ops", "web", "templates", "partials", "artists.html")
CAND = os.path.join(ROOT, "ops", "web", "templates", "partials", "artist-candidates.html")
MAIN = os.path.join(BACKEND, "cmd", "server", "main.go")

# Both packages. The service can be broken with no route changing, and a route
# or a template can stop asking with the service unchanged.
PKGS = ["./internal/services", "./internal/api"]
TEST_FILTER = ("AddMonitoredArtist|QueueArtistScan|DeleteMonitoredArtist|ArtistsHandler"
               "|EveryArtistSyncButton|NoTemplateDependsOnEval|ShippedCSPDoesNotOfferUnsafeEval")

BS = chr(92)
TAB = chr(9)
NL = chr(10)
CR = chr(13)


class StaleAnchor(SystemExit):
    """The code moved. Better a loud stop than a silent no-op that reports green."""


def require(cond, message):
    if not cond:
        raise StaleAnchor("STALE ANCHOR: " + message)


def load(path):
    """Return (text with LF endings, the ending to write back).

    Every file here is pure CRLF in a Windows checkout and pure LF in CI. A
    harness that hardcodes one of them matches nothing on the other, and a
    mutation that changes nothing reports as a miss. Purity is asserted rather
    than normalised, so a file with mixed endings cannot round-trip silently.
    """
    with io.open(path, encoding="utf-8", newline="") as fh:
        raw = fh.read()
    crlf_count = raw.count(CR + NL)
    bare_lf = raw.count(NL) - crlf_count
    bare_cr = raw.count(CR) - crlf_count
    # Reject only GENUINELY mixed endings. Demanding zero bare LF, as a first
    # version of this did, rejects the CI checkout too -- Linux has no CRLF at
    # all, so every one of its lines is a "bare LF" and the harness refused to
    # start. That is the same trap as the sibling's: every CRLF file also
    # contains LF, so a bare-LF test is the wrong test in both directions.
    require(not (crlf_count and (bare_lf or bare_cr)),
            "%s has mixed line endings (%d CRLF, %d bare LF, %d bare CR); refusing to guess"
            % (os.path.relpath(path, ROOT), crlf_count, bare_lf, bare_cr))
    return raw.replace(CR + NL, NL), crlf_count > 0


def save(path, text_lf, crlf):
    out = text_lf.replace(NL, CR + NL) if crlf else text_lf
    with io.open(path, "w", encoding="utf-8", newline="") as fh:
        fh.write(out)


def go_test():
    """Run the promise's tests. PORT is cleared: this environment exports
    PORT=0, which breaks internal/config's own defaults test."""
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


def sub(text, old, new, nth=1):
    """Replace the nth occurrence. Two mutations that differ only in WHICH of
    two identical literals they hit are the same mutation."""
    parts = text.split(old)
    require(len(parts) > nth,
            "anchor %r occurs %d times, need more than %d" % (old[:60], len(parts) - 1, nth))
    return old.join(parts[:nth]) + new + old.join(parts[nth:])


# --------------------------------------------------------------------------
# Anchors, verified against the code as it stands.
# --------------------------------------------------------------------------

SVC_SRC, SVC_CRLF = load(SVC)
TPL_SRC, TPL_CRLF = load(TPL)
CAND_SRC, CAND_CRLF = load(CAND)
MAIN_SRC, MAIN_CRLF = load(MAIN)

ENQUEUE_CALL = (
    TAB * 2 + 'if _, _, err := s.queueArtistScan(tx, &artist, "user_api"); err != nil {' + NL +
    TAB * 3 + 'return err' + NL +
    TAB * 2 + '}' + NL
)
ACTIVE_CHECK = (
    TAB + 'switch {' + NL +
    TAB + 'case err == nil:' + NL +
    TAB * 2 + 'return &existing, true, nil' + NL
)
RELEASE_DELETE = (
    TAB * 2 + 'if err := tx.Where("artist_id IN (?)", scope.Select("id")).Delete(&database.TrackedRelease{}).Error; err != nil {' + NL +
    TAB * 3 + 'return err' + NL +
    TAB * 2 + '}' + NL
)
JOB_TYPE_LINE = TAB * 2 + 'Type:        "artist_scan",' + NL
JOB_SCOPE_LINE = TAB * 2 + 'ScopeType:   "artist",' + NL
CREATE_LINE = TAB + 'if err := db.Create(&job).Error; err != nil {' + NL
TPL_SYNC = 'hx-post="/api/artists/{{ artist.ID }}/sync" hx-target="#notice"'

for probe, where in (
    (ENQUEUE_CALL, "the enqueue call inside AddMonitoredArtist's transaction"),
    (ACTIVE_CHECK, "the already-active switch in queueArtistScan"),
    (RELEASE_DELETE, "the tracked-release delete in DeleteMonitoredArtist"),
    (JOB_TYPE_LINE, "the job type in the queued Job literal"),
    (JOB_SCOPE_LINE, "the job scope type in the queued Job literal"),
    (CREATE_LINE, "the job insert in queueArtistScan"),
    (TPL_SYNC, "the artists-list Sync button's hx-target"),
):
    require(probe in SVC_SRC if where != "the artists-list Sync button's hx-target" else probe in TPL_SRC,
            "cannot find %s" % where)

require(TPL_SRC.count(TPL_SYNC) == 1,
        "expected exactly one artists-list Sync button, found %d" % TPL_SRC.count(TPL_SYNC))

Q = chr(39)  # quotes, built rather than escaped: every anchor below is full of
DQ = chr(34) # them, and an escaped literal is exactly what a patch script
             # mangles on the way to disk.

CAND_ROW = "hx-include=" + DQ + "closest [role=" + Q + "listitem" + Q + "]" + DQ
EVAL_ATTR = " hx-vals=" + Q + "js:{name: 1}" + Q
MAIN_POLICY = "script-src " + Q + "self" + Q

require(CAND_SRC.count(CAND_ROW) == 1,
        "expected the candidate row to collect its values with hx-include,"
        "found %d"
        % CAND_SRC.count(CAND_ROW))
require(MAIN_SRC.count(MAIN_POLICY) >= 1,
        "could not find the script-src directive in main.go")


# --------------------------------------------------------------------------
# Mutations. Each one is a defect class a future edit could plausibly
# introduce, and each must still COMPILE -- a mutation that fails the compiler
# is VOID and proves nothing.
# --------------------------------------------------------------------------

def m_add_never_queues(svc, tpl, cand, main):
    return svc.replace(ENQUEUE_CALL, ""), tpl, cand, main


def m_enqueue_never_writes(svc, tpl, cand, main):
    # `error(nil)` compiles and is always nil, so the insert never runs and the
    # function hands back a zero Job instead of an error.
    return svc.replace(CREATE_LINE, TAB + 'if err := error(nil); err != nil {' + NL), tpl, cand, main


def m_scan_scoped_to_library(svc, tpl, cand, main):
    return svc.replace(JOB_SCOPE_LINE, TAB * 2 + 'ScopeType:   "library",' + NL), tpl, cand, main


def m_wrong_job_type(svc, tpl, cand, main):
    return svc.replace(JOB_TYPE_LINE, TAB * 2 + 'Type:        "scan",' + NL), tpl, cand, main


def m_always_queues_a_second(svc, tpl, cand, main):
    return svc.replace(ACTIVE_CHECK, TAB + 'switch {' + NL + TAB + 'case false:' + NL + TAB * 2 + 'return &existing, true, nil' + NL), tpl, cand, main


def m_enqueue_error_is_swallowed(svc, tpl, cand, main):
    # The row lands even when its scan could not be queued: the defect DJI-588
    # is about, one level down.
    return svc.replace(ENQUEUE_CALL, TAB * 2 + 's.queueArtistScan(tx, &artist, "user_api")' + NL), tpl, cand, main


def m_delete_leaves_releases_behind(svc, tpl, cand, main):
    return svc.replace(RELEASE_DELETE, ""), tpl, cand, main


def m_list_button_discards_the_answer(svc, tpl, cand, main):
    return svc, tpl.replace(TPL_SYNC, 'hx-post="/api/artists/{{ artist.ID }}/sync" hx-target="#artist-{{ artist.ID }}" hx-swap="none"'), cand, main


def m_picker_needs_eval_again(svc, tpl, cand, main):
    # The defect itself: the pick rebuilt as an expression htmx compiles with
    # eval, which script-src 'self' forbids.
    return svc, tpl, cand.replace(CAND_ROW, CAND_ROW + EVAL_ATTR), main



def m_policy_offers_unsafe_eval(svc, tpl, cand, main):
    # The wrong fix, applied to the header instead of the template. It really
    # does make the picker work -- which is why it needs a guard rather than a
    # comment: it is the cheapest-looking way back to a green browser.
    return svc, tpl, cand, main.replace(MAIN_POLICY,
                                          "script-src " + Q + "self" + Q
                                          + " " + Q + "unsafe-eval" + Q, 1)



MUTATIONS = [
    ("M1 adding an artist queues nothing (the defect)", m_add_never_queues),
    ("M2 the enqueue writes no job", m_enqueue_never_writes),
    ("M3 the scan is scoped to a library", m_scan_scoped_to_library),
    ("M4 the job is queued as a library scan", m_wrong_job_type),
    ("M5 asking again always queues a second", m_always_queues_a_second),
    ("M6 a failed enqueue leaves the artist behind", m_enqueue_error_is_swallowed),
    ("M7 removing a scanned artist leaves its releases", m_delete_leaves_releases_behind),
    ("M8 the Sync button throws the answer away", m_list_button_discards_the_answer),
    ("M9 the picker needs eval again", m_picker_needs_eval_again),
    ("M10 the policy offers unsafe-eval", m_policy_offers_unsafe_eval),
]

# Every mutant must be a DIFFERENT file state. Two identical states mean the
# mutations are not testing what they claim to be testing.
STATES = {}
for label, mutate in MUTATIONS:
    result = mutate(SVC_SRC, TPL_SRC, CAND_SRC, MAIN_SRC)
    require(all(isinstance(part, str) for part in result),
            "%s did not produce text" % label)
    require(tuple(result) != (SVC_SRC, TPL_SRC, CAND_SRC, MAIN_SRC),
            "%s changed nothing in any of the four files" % label)
    key = tuple(result)
    require(key not in STATES,
            "%s produces a file state another mutation already produced -- "
            "they are one mutation, and one of them is not being proved" % label)
    STATES[key] = label


def apply(state):
    save(SVC, state[0], SVC_CRLF)
    save(TPL, state[1], TPL_CRLF)
    save(CAND, state[2], CAND_CRLF)
    save(MAIN, state[3], MAIN_CRLF)


def restore_ok():
    """Read the files back rather than trusting that apply() was called: a
    write that fails, or one that lands in the wrong encoding, is exactly the
    case this has to catch."""
    now = tuple(load(p)[0] for p in (SVC, TPL, CAND, MAIN))
    return now == (SVC_SRC, TPL_SRC, CAND_SRC, MAIN_SRC)


def main():
    print("Go-level mutation proof: a monitored artist is scanned, and its picker")
    print("                 can be operated under the CSP this app actually serves")
    print("  owner    %s" % os.path.relpath(SVC, ROOT))
    print("  surface  %s" % os.path.relpath(TPL, ROOT))
    print("  picker   %s" % os.path.relpath(CAND, ROOT))
    print("  policy   %s" % os.path.relpath(MAIN, ROOT))
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
            apply(mutate(SVC_SRC, TPL_SRC, CAND_SRC, MAIN_SRC))
            status, failed, _ = classify(go_test())
            show(status, failed)
            results.append((label, status, failed))
    finally:
        apply((SVC_SRC, TPL_SRC, CAND_SRC, MAIN_SRC))

    if not restore_ok():
        sys.stderr.write("RESTORE FAILED: a source file is not byte-identical to "
                         "its pre-mutation content. Nothing after this point is "
                         "trustworthy -- run `git status` before anything else.\n")
        return 2
    print("\nRestore proven byte-identical for all four files.")

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

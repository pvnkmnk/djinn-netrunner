"""Mutation check for the DJI-571 request-indicator guard.

Every mutation must be CAUGHT (the guard's test fails) and every control must
PASS. Mutations restore from an in-process snapshot, never `git checkout --`,
which would wipe unrelated uncommitted work. A `[setup failed]`/`build failed`
is VOID, not caught - the harness distinguishes them explicitly.

Run from the repo root:  python scripts/dji571_mutation_check.py
"""

import io
import os
import re
import subprocess
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

REPO = os.getcwd()
BACKEND = os.path.join(REPO, "backend")
PKG = "./internal/api/templates/"

APPJS = os.path.join(REPO, "ops", "web", "static", "js", "app.js")
CSS = os.path.join(REPO, "ops", "web", "static", "css", "styles.css")
WL_CARD = os.path.join(REPO, "ops", "web", "templates", "partials", "watchlist-card.html")
ARTIST_CARD = os.path.join(REPO, "ops", "web", "templates", "partials", "artist-card.html")
ARTISTS = os.path.join(REPO, "ops", "web", "templates", "partials", "artists.html")
LIBS = os.path.join(REPO, "ops", "web", "templates", "partials", "libraries.html")

GUARD_TESTS = {
    "clearing_path": "TestTheRequestIndicatorIsClearedWhenTheRequestEnds",
    "busy_state": "TestTheBusyStateIsStillDeclaredInTheStylesheet",
    "non_vacuity": "TestTheControlsThatTriggerTheSharedCounterStillCarryIt",
}

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
    # Anchors are written with CRLF because that is how these assets sit in the
    # working copy here, but git may check any of them out with LF. Normalising
    # both sides to the file's own newline keeps an anchor from silently going
    # stale and aborting the run at the first mutation - which would report no
    # mutations at all rather than reporting them.
    newline = "\r\n" if "\r\n" in text else "\n"
    old = old.replace("\r\n", "\n").replace("\n", newline)
    new = new.replace("\r\n", "\n").replace("\n", newline)
    if text.count(old) != count:
        raise SystemExit(
            "ANCHOR STALE (%d != %d) in %s:\n%s" % (text.count(old), count, path, old[:200])
        )
    with io.open(path, "wb") as fh:
        fh.write(text.replace(old, new).encode("utf-8"))


def run_guard():
    """Return (ok, failing_test_names, saw_build_error)."""
    env = dict(os.environ)
    env["PORT"] = ""
    proc = subprocess.run(
        ["go", "test", PKG, "-run", "|".join(GUARD_TESTS.values()), "-v", "-count=1"],
        cwd=BACKEND, env=env, capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    out = proc.stdout + proc.stderr
    failed = sorted(set(re.findall(r"^--- FAIL: (\w+)", out, re.M)))
    void = ("[build failed]" in out) or ("build failed" in out) or ("cannot find" in out)
    return proc.returncode == 0, failed, void


# ---------------------------------------------------------------- mutations

def m1_remove_listener():
    patch(APPJS, "    document.body.addEventListener('htmx:afterRequest', function (evt) {\r\n        clearRequestIndicator(evt.detail && evt.detail.elt);\r\n    });\r\n", "")


def m2_wrong_event():
    patch(APPJS, "addEventListener('htmx:afterRequest'", "addEventListener('htmx:beforeRequest'")


def m3_listener_becomes_noop():
    patch(APPJS, "        clearRequestIndicator(evt.detail && evt.detail.elt);",
          "        void evt;")


def m4_removal_removed():
    patch(APPJS, ".forEach(function (name) { node.classList.remove(name); });",
          ".forEach(function (name) { void name; });")


def m5_prefix_family_dropped():
    patch(APPJS, "return name === 'htmx-request' || name.indexOf('htmx-request-') === 0;",
          "return name === 'htmx-request';")


def m6_busy_state_rule_deleted():
    patch(CSS, ".htmx-request {\r\n    opacity: 0.7;\r\n    cursor: wait !important;\r\n}",
          ".htmx-request-removed {\r\n    opacity: 0.7;\r\n}")


def m7_all_attributes_removed():
    for path in (WL_CARD, ARTIST_CARD, ARTISTS, LIBS):
        with io.open(path, "rb") as fh:
            text = fh.read().decode("utf-8")
        assert "hx-disabled-elt" in text, path
    for path in (WL_CARD, ARTIST_CARD, ARTISTS, LIBS):
        snapshot(path)
        with io.open(path, "rb") as fh:
            text = fh.read().decode("utf-8")
        with io.open(path, "wb") as fh:
            fh.write(text.replace(' hx-disabled-elt="this"', "").encode("utf-8"))


def m8_wrong_elt():
    # clears a hard-coded element instead of the one that issued the request
    patch(APPJS, "        clearRequestIndicator(evt.detail && evt.detail.elt);",
          "        clearRequestIndicator(document.getElementById('btn-attach'));")


# ----------------------------------------------------------------- controls

def c1_benign_js():
    patch(APPJS, "setTimeout(() => copyBtn.textContent = 'Copy Last 200', 2000);",
          "setTimeout(() => copyBtn.textContent = 'Copy Last 200', 2500);")


def c2_benign_template():
    patch(WL_CARD, 'aria-label="Sync watchlist {{ watchlist.Name }}"',
          'aria-label="Sync this watchlist ({{ watchlist.Name }})"')


def c3_benign_css():
    patch(CSS, ".btn-sm {\r\n    padding: 0.25rem 0.5rem;", ".btn-sm {\r\n    padding: 0.3rem 0.5rem;")


MUTATIONS = [
    ("M1", "afterRequest listener deleted", m1_remove_listener, "clearing_path"),
    ("M2", "listener bound to beforeRequest", m2_wrong_event, "clearing_path"),
    ("M3", "listener no longer calls the clearer", m3_listener_becomes_noop, "clearing_path"),
    ("M4", "clearing routine stops removing the class", m4_removal_removed, "clearing_path"),
    ("M5", "htmx-request-* family no longer cleared", m5_prefix_family_dropped, "clearing_path"),
    ("M6", "busy-state CSS rule deleted", m6_busy_state_rule_deleted, "busy_state"),
    ("M7", "every hx-disabled-elt removed", m7_all_attributes_removed, "non_vacuity"),
    ("M8", "listener clears a hard-coded element", m8_wrong_elt, "clearing_path"),
]

CONTROLS = [
    ("C1", "benign app.js constant change", c1_benign_js),
    ("C2", "benign template aria-label change", c2_benign_template),
    ("C3", "benign css padding change", c3_benign_css),
]

results = []


def record(kind, ident, desc, expect_fail, target):
    ok, failed, void = run_guard()
    if void:
        results.append((kind, ident, desc, "VOID", failed, target))
    elif expect_fail:
        caught = ok is False and any(t == target for t in failed)
        results.append((kind, ident, desc, "CAUGHT" if caught else "MISSED", failed, target))
    else:
        passed = ok and not failed
        results.append((kind, ident, desc, "PASS" if passed else "BROKE", failed, target))


try:
    print("baseline ...", flush=True)
    ok, failed, void = run_guard()
    print("  baseline:", "GREEN" if ok and not void else "NOT GREEN", failed, flush=True)
    baseline_green = ok and not failed and not void

    for ident, desc, fn, target in MUTATIONS:
        restore_all()
        fn()
        record("MUT", ident, desc, True, GUARD_TESTS[target])
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
for kind, ident, desc, verdict, failed, target in results:
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
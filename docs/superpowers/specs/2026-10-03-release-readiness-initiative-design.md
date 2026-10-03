# NetRunner Release Readiness Initiative — Design

- **Date:** 2026-10-03
- **Status:** Design approved; awaiting implementation plan
- **Path:** architectural (brainstorming skill)
- **Release target:** public open-source release
- **Working agreement:** full autonomy — commit, push, open and merge per stage once its exit criterion is met

---

## 1. Premise

NetRunner's e2e gate is not "five red tests." Per DJI-596, the Playwright
browser suite **covered nothing at all** since DJI-544 landed: the fixture
declared passwords below the registration floor, so `ensureUserExists` threw
and every authenticated spec failed during setup — while specs that never
touched the fixture kept passing. A suite whose fixture throws still reports a
plausible number of green tests. Three separate slices (DJI-545, DJI-590,
DJI-571) each had to record that the e2e suite could not be run locally.

DJI-595 compounds it: a browser-worker crash (`0xC0000409`) abandons 176 tests
mid-run, so the suite reports a failure count that understates reality and a
"did not run" population that is not a coverage measurement.

The consequence for planning: **no stage after Stage 1 can claim to be
verified.** This is why the gate is the first stage and not a parallel
workstream. It is also this repository's own repeated lesson — *a guard whose
fixture cannot reach the code is not a guard; it is a test that the test ran.*

## 2. Starting position

Linear held 275 active issues against a 250 free-plan limit.

| | before | after |
| --- | --- | --- |
| Active | 275 | **37** |
| Archived | 311 | 549 |
| Total | 586 | **586** |

Cleanup archived rather than deleted, so nothing was destroyed and every
change is reversible with `issueUnarchive`:

- **172** terminal-state NetRunner issues (Done / Canceled / Duplicate)
- **66** AGE-team issues — a separate homelab-network project (Proxmox, DNS,
  segmentation), unrelated to NetRunner

Verified afterwards by a fresh paginated query, not by trusting each
mutation's `success` flag: all 37 remaining issues were confirmed active, with
zero active issues unlisted, and spot-checks confirmed archived issues were
archived rather than deleted.

The 37 remaining issues decompose as:

| Group | Count | Location |
| --- | --- | --- |
| e2e gate integrity (DJI-592/593/594/595/596) | 5 | P-DJI-28 |
| Product bugs (DJI-555, 559, 562, 583, 588, 589) | 6 | P-DJI-28 |
| Subsonic/OpenSubsonic conformance, NR01–NR25 | 25 | P-DJI-29 |
| Straggler (DJI-516, ga-probes library precondition) | 1 | P-DJI-14 |

## 3. Goals and non-goals

**Goals**

1. A full-suite e2e run that completes and whose result means something.
2. No known product defect that a public user would hit on a first session.
3. A UI that holds up for an external audience.
4. Subsonic/OpenSubsonic conformance good enough for real third-party clients.
5. A stranger reaches acquired music from a fresh clone, unaided.

**Non-goals**

- NetRunner-specific native client work. P-DJI-29 scopes are server-only and
  client-agnostic; that boundary is respected.
- Speculative refactoring. Each stage fixes or polishes what serves the goal.
- Any NetRunner ticket outside the 37 without being raised as new work.

## 4. Stage sequence

Chosen over two alternatives: A (single polish pass before conformance) leaves
the UI untouched until all 25 Subsonic issues land; B (conformance before
polish) risks never reaching the UI at all. The chosen shape takes two bites at
polish so neither is squeezed by the conformance epic.

### Stage 1 — Make the instrument trustworthy

**Objective:** a full-suite run completes, and its result is evidence.

**Work**

- Land the in-flight branch `fix/e2e-job-type-add-form-submit-nav` (4 commits:
  DJI-592, DJI-593, DJI-594, and the nav-count orphan).
- File the orphan ticket for `auth.spec.ts:660` and close it with the branch.
  Diagnosis: `base.html` renders 8 unconditional nav links plus one gated
  behind `{% if IsAdmin %}`. The spec asserted 9 for both roles, so it was green
  on a privilege leak and red on correct behaviour — spec drift of the same
  class as DJI-592, but inverted. `authenticatedPage` uses `TEST_USER`, which
  nothing promotes, so a regular user correctly sees 8.
- **DJI-595** — resolve the browser-worker crash and make unexplained
  not-runs and skips fail the run rather than sit in the output as a line an
  operator has to notice.
- **DJI-516** — seed the library precondition the success probe silently
  depends on, and make the assertion fail loudly on a missing library rather
  than reporting "the track is not visible".
- Close DJI-596 as merged.

**Approach to DJI-595:** rather than chase a Windows fast-fail whose cause is
unknown, shard the suite so no single worker accumulates the crash. Sharding is
also strictly better for wall-clock. If sharding does not hold, isolate the
offending spec by elimination.

**Exit criterion:** a full-suite Playwright run completes with **zero "did not
run" and zero unexplained skips**, all green, on CI and locally — and per-file
runs agree with the whole.

*Definition, so this cannot be read conveniently:* a skip is **explained** only
if it is a declared conditional skip whose reason is recorded at the call site.
Any test that does not run — because a worker died, a fixture threw, or a
dependency was unavailable — is unexplained and fails the run. There is no
"expected" bucket that grows quietly.

**Risk:** the crash's root cause is unproven. Mitigation is the sharding
fallback above. If neither holds, Stage 1 does not close on a partial run, and
that must be reported rather than waived.

### Stage 2 — Correct the product bugs

**Objective:** no known defect a public user hits on a first session.

Ordered by public-release impact.

1. **DJI-559** — registration returns `500` for any password over bcrypt's
   72-byte limit. Unreachable by UI hint, trivially produced by a passphrase
   generator. Decision: reject with a `400` naming the limit explicitly.
   Pre-hashing is rejected — it changes the credential format and forces
   two-scheme verification on every login, which is a migration, not a patch.
   The message must state the unit, because the floor counts runes and this
   ceiling counts bytes: a password can clear a 12-rune floor and still exceed
   72 bytes. Floor and ceiling read one shared `config.BcryptMaxPasswordBytes`.
2. **DJI-588** — adding a monitored artist is a bare insert; nothing scans it,
   and there is no surface to ask for a scan. This is the product's core
   promise stopping short. Queue the scan on add, give the operator an on-demand
   scan control, and assert the scan reaches the queue.
3. **DJI-589** — the artist card shows no MusicBrainz provenance and cannot be
   re-pointed at a different entity. Show disambiguation, country and type from
   data the service already decodes, and add an edit control reusing the DJI-548
   candidate picker. This repairs rows created before the picker shipped — the
   `Napalm Death` row from typing "Death" is the proof case.
4. **DJI-583** — owner-less jobs are silently omitted, presenting an owner-scoped
   list as the whole queue. Make the truncation visible. Write scope (Cancel,
   Retry) stays unchanged; only read presentation changes.
5. **DJI-562** — the console stream connects to `/ws/jobs`, which has no route.
   Real feature work per the ticket: build a job-selection surface over the
   settled jobs queue. The server route is already correct and secured.
6. **DJI-555** — see §6.

**Exit criterion:** each bug has a Go guard and e2e proof, and
`docs/DEPLOYMENT.md` matches what was decided.

### Stage 3 — UI polish (fast)

**Objective:** nothing visibly broken, found by looking rather than guessing.

With a working gate, sweep every page in a real browser: dead and empty states
(including the dashboard console), error surfacing on 4xx/5xx, modal and form
correctness, responsive behaviour. Fix what is cheap and visible; file tickets
for the rest.

**Exit criterion:** every page reviewed with findings recorded; each finding
either fixed or filed, with no finding left unrecorded.

### Stage 4 — Subsonic/OpenSubsonic conformance (NR01–NR25)

**Objective:** conformance good enough for real third-party clients.

Executed in the dependency order the issues already record:

1. **NR01** (DJI-560) audit first — it determines the real scope and may
   legitimately retire or reshape tickets.
2. Foundations: NR02 contracts, NR03 auth lifecycles, NR04 unified
   authorization, NR05 discovery, NR06 identity/browsing, NR08 metadata and
   artwork.
3. Feature groups: NR07, NR09–NR16, NR19, NR20, NR22–NR25.
4. **NR17** (DJI-578) conformance gate — every other issue blocks it.
5. **NR18** (DJI-579) evidence publication, last.

**Exit criterion:** `docs/OPENSUBSONIC_COVERAGE.md` is truthful, NR17 is
green, and real third-party clients are verified — headless conformance alone
is explicitly not sufficient evidence.

**Risk:** this is 25 dependency-ordered issues and can swallow the initiative.
Stages 1–3 and 6 are independently shippable regardless, and NR01's audit is
the honest lever for cutting its true size.

### Stage 5 — UI polish (deep)

**Objective:** release quality for an external audience.

Accessibility, keyboard navigation, contrast, error copy, and empty states for
surfaces conformance settled. This is where "release quality" is actually
earned for people who did not build it.

**Exit criterion:** a reviewed accessibility and keyboard pass over every page,
with findings fixed or filed.

### Stage 6 — Release

**Objective:** a stranger succeeds.

- Security review and `govulncheck`.
- The DJI-555 decision settled either way, and recorded.
- Stranger test: clone → configure → run → acquire music, unaided.
- Upgrade, backup and restore paths documented and tested.
- Setup docs, CHANGELOG, release tagging via `scripts/version.sh`.
- `scripts/smoke.sh` green against a built stack.
- All three mutation checks green (`gate`, `boundary`, `success`).

**Exit criterion:** a stranger reaches acquired music from a fresh clone
without help.

## 5. Cross-cutting rules

- **One branch per stage**, one PR, merged once the stage's exit criterion is
  met. Linear is reconciled from the merged commit, never from intent.
- **Exit criteria are behavioural proofs, never a passing count.** A stage
  closes on an observed behaviour, not on a green number whose meaning was
  never checked.
- **Ticket state follows merged reality.** Set from the merged commit, with the
  hash as evidence.
- **Established facts before edits.** For every change, read the template,
  handler or source that determines correct behaviour *before* writing the
  assertion. DJI-591 is the precedent: a naive swap that satisfied the ticket's
  wording silently broke passing tests.
- **Report failures; never waive them.** If a stage's exit criterion cannot be
  met, say which checks failed and why rather than relaxing the check.

## 6. Open decision — DJI-555 bootstrap admin

`BOOTSTRAP_ADMIN_EMAIL` proves an *address* was configured, not that the person
who registered it is the operator. Registration is open in every environment,
so an anonymous client knowing the configured address can claim `admin` first.

The ticket's own reasoning deferred this as proportionate risk on the grounds
that NetRunner is self-hosted and single-machine, with the boundary being
control of `.env` on the host rather than the network. **That premise does not
hold for a public open-source release**, which is why it is surfaced here
rather than inherited.

Options, to be decided at Stage 2 planning:

1. **Invite/enrollment secret** — a second env var the registrant must present.
   Proportionate to the revised threat model. Recommended.
2. **Email verification for the bootstrap address only** — the narrow, correct
   fix, but needs a verification flow, an SMTP dependency and a token table.
3. **Record a decision not to fix**, with reasoning and a threat model written
   into `docs/DEPLOYMENT.md`.

The ticket's acceptance criteria require the threat model to be written down
either way, so option 3 is a legitimate outcome — but it must be a recorded
decision, not an omission. Any fix must cover both the registration-time and
the startup promotion path, and must not add a promotion trigger to the
enumeration-safe `201` duplicate branch.

## 7. Risk register

| Risk | Stage | Mitigation |
| --- | --- | --- |
| DJI-595 crash cause unproven | 1 | Shard the suite; fall back to isolation. Stage does not close on a partial run. |
| Orphan nav-count spec drift misdiagnosed | 1 | Diagnosis recorded before the fix; the fix asserts both the count and the absence of the admin link. |
| Conformance epic swallows the initiative | 4 | Stages 1–3 and 6 ship independently; NR01's audit sizes the real work. |
| Public release raises DJI-555's stakes | 2 | Threat model decided explicitly, not inherited. |
| UI polish deferred indefinitely | 3, 5 | Two passes, each with its own exit criterion. |
| Test runs on stale built images | all | Templates, CSS and JS are baked into images; rebuild before trusting a run. |

## 8. Out of scope for this initiative

- NetRunner-specific native client implementation.
- The retired beta path. Dev and release are the only paths.
- Any change to `AGENTS.md` unless separately requested.

## 9. Decomposition and planning

This document is the **initiative** design, not an implementation plan. It spans
six stages across the whole product, so a single implementation plan covering
it would be too large to execute usefully.

Each stage gets **its own spec-to-plan-to-implementation cycle**:

1. Plan **Stage 1** first, and execute it to its exit criterion.
2. At each later stage, write that stage's plan against the state actually
   reached — not against this document's prediction of it.

Stage boundaries carry the dependency, not the plan: Stage 2's plan is written
knowing Stage 1's gate is trustworthy, and Stage 4's is written knowing Stage
3's UI findings exist. NR01's audit in Stage 4 may legitimately resize Stage 4,
which is a reason to plan it late rather than early.

**Immediately next step:** an implementation plan for Stage 1 only.
# DJI-626 — Are the three test-infrastructure defects still live?

**Verdict: all three are still live.** This is the highest-value cluster in the
research set, because every one of them is a defect in a *guard* rather than in
the product: each makes a green tick mean less than it reads as meaning.

---

## Claim A — DJI-602: the mutation harness runs a hand-picked package list

**Verdict: still live.**

### Evidence

The package list *is* the whole package selection:

- `scripts/artist_scan_mutation_check.py:63`
  ```python
  PKGS = ["./internal/services", "./internal/api"]
  ```
- `scripts/artist_scan_mutation_check.py:117` — `PKGS` is the whole package selection:
  ```python
  ["go", "test"] + PKGS + ["-run", TEST_FILTER, "-count=1"],
  ```
- `scripts/password_policy_mutation_check.py:70`
  ```python
  PKGS = ["./internal/config", "./internal/api"]
  ```

The artist-scan harness mutates **two template files** — it is not a
Go-source-only harness:

- `scripts/artist_scan_mutation_check.py:57-59` bind `TPL` to
  `ops/web/templates/partials/artist-card.html`, `CAND` to
  `.../artist-candidates.html` and `MAIN` to `backend/cmd/server/main.go`; `apply()` writes them.
- `M8 "the Sync button throws the answer away"` rewrites the artist card's Sync
  button; `M9 "the picker needs eval again"` rewrites the candidate row; `M10`
  rewrites the shipped CSP in `backend/cmd/server/main.go`.

The package whose tests read **those exact files** off disk is a *separate Go
package* that neither `PKGS` runs:

- `backend/internal/api/templates/artist_card_actions_test.go:1`
  ```go
  package templates
  ```
  and its guard asserts precisely what M8 breaks —
  `artist_card_actions_test.go:108-109`
  ```go
  {"Sync", "POST /api/artists/a1/sync", "#notice"},
  {"Re-point", "POST /api/artists/search", "#modal-container"},
  ```
- Every `backend/internal/api/templates/*_test.go` declares `package templates`
  (`ui_honesty_test.go`, `htmx_target_coverage_test.go`, `csp_style_test.go`,
  `artist_provenance_test.go`, `stylesheet_coverage_test.go`, `escaping_test.go`).

The harness's own named tests do *not* secretly live in the omitted package —
checked, so the omission is the only defect here:

- `EveryArtistSyncButton` → `backend/internal/api/artist_sync_template_test.go:1`
  `package api`
- `NoTemplateDependsOnEval` → `backend/internal/api/csp_template_test.go:48`
- `ShippedCSPDoesNotOfferUnsafeEval` → `backend/internal/api/csp_template_test.go:116`
  (`csp_template_test.go:1` is `package api`)

So `./internal/api` does catch the template mutations — that is why the harness
reports green — while a *second, independent* guard on the same files is never
run and its failures are invisible.

No docstring declares the exclusion, which is what makes it a defect rather than
a documented choice. `scripts/artist_scan_mutation_check.py`'s "Scope note" says:

> "What IS here is provable without a running stack: the template mutations are
> caught at the Go level because the guard tests read the templates and the CSP
> header from disk."

It names neither `backend/internal/api/templates` nor an exclusion.
`scripts/password_policy_mutation_check.py` names no exclusion at all.

The measured consequence is already recorded in-repo — `AGENTS.md:733-739`:

> **A hand-written package list is the weakest link in a harness.**
> `PKGS = ["./internal/services", "./internal/api"]` omits
> `backend/internal/api/templates`, which reads the same templates off disk — so
> the harness reported `10/10 mutations caught; both controls green` while that
> package failed four tests on the very file M8 mutates. A green tick means
> "those packages"; CI reads it as "the promise". Derive the list or declare the
> exclusion in the docstring.

Two further inventories from reading:

- `scripts/mutation-check.sh` has **no package list** — it is the browser-level
  harness and selects a spec by grep (`SPEC="e2e/tests/ga-probes.spec.ts"`,
  `test_grep_for()`), so it is not part of this defect.
- A third harness *does* target the package, but it is **not wired into CI**:
  `scripts/dji571_mutation_check.py:21` `PKG = "./internal/api/templates/"`.
  `.github/workflows/ci.yml` runs only the password-policy and artist-scan steps.

### Disposition — and the answer DJI-602 specifically asks for

**Fix now.** Packages missing from the CI-wired harnesses today:

| Harness | Missing package | Live? |
| --- | --- | --- |
| `artist_scan_mutation_check.py` | `./internal/api/templates` | **yes** — it guards the same files M8/M9 mutate |
| `artist_scan_mutation_check.py` | `./cmd/server` | benign — its only mutated file is `main.go`, and M10 is caught from `internal/api` by `TestShippedCSPDoesNotOfferUnsafeEval` |
| `password_policy_mutation_check.py` | `./internal/api/templates` | benign — no mutated file is a template |

So the fix is **both**: derive the list (e.g. `go list ./...` filtered the way
`ci.yml` filters `/integration/`) *and* declare any intentional exclusion in the
docstring. Deriving alone would change the harness's runtime and its meaning;
declaring alone leaves the gap open. AGENTS.md already prescribes exactly this
pair, which means the ticket does not need a new design decision.

---

## Claim B — DJI-604: `internal/services` vs go test's 10-minute default

**Verdict: still live.**

### Evidence

`scripts/validate.sh:13-14` — no `-timeout`:
```bash
echo "[validate] go test ./..."
go test ./...
```

`scripts/validate.ps1:17-18` — same:
```powershell
Write-Host "[validate] go test ./..."
go test ./...
```

`.github/workflows/ci.yml:29` — the CI `test` job, also no `-timeout`:
```yaml
run: go test $(go list ./... | grep -v /integration/) -coverprofile=coverage.out
```

The only places a timeout is set at all are the integration paths, which is why
they look covered while the unit suite is not:

- `scripts/integration-tests.sh:168` `test_args=(-v -tags=integration -timeout "$TIMEOUT")`
- `.github/workflows/integration.yml:97` `-timeout=12m`

The hazard is measured, not theoretical — `AGENTS.md:807-810`:

> **`internal/services` can exceed go test's default 10m0s.** One ffprobe-backed
> download-probe test ran 9m46s on a loaded box while the package totals 177s
> unloaded and CI's whole `test` job 5m24s, so `panic: test timed out` there is
> a wall-clock hazard, not a failure: use `go test ./internal/services -timeout 30m`.

The ffprobe-backed tests are *conditional*, so the hazard is load- and
environment-dependent rather than always-on:
`backend/internal/services/audio_probe_test.go:19-27`
```go
func requireProbeTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
}
```
They **skip** without the toolchain and **run** with it — and CI installs ffmpeg —
so the slow path is the CI path and the developer path, while only a bare
checkout is spared.

### Disposition

**Fix now.** Add an explicit `-timeout` to `scripts/validate.sh`, `scripts/validate.ps1`
and the `ci.yml` test step (30m is the value AGENTS.md already prescribes for
this package). A `panic: test timed out` reported as a red `test` job is a
false negative that trains people to re-run rather than read.

---

## Claim C — DJI-608: a local Playwright run leaves rows in the reused e2e DB

**Verdict: still live** (narrower than the ticket's wording — cleanup is
partly automated, but the reuse mode is unchanged and unowned rows persist).

### Evidence

The reuse switch, and the teardown that is skipped because of it:

- `e2e/playwright.config.ts:47`
  ```typescript
  reuseExistingServer: !process.env.CI,
  ```
- `e2e/playwright.config.ts:54` `globalTeardown: './teardown.ts',`
- `e2e/teardown.ts:4-7`
  ```typescript
  if (!process.env.CI) {
    console.log('Skipping Docker teardown in local dev (reuseExistingServer is enabled).');
    return;
  }
  ```

The fresh database exists, but only as the `webServer.command`, which Playwright
**skips entirely** when the health URL already answers and `reuseExistingServer`
is true — which is exactly the local case:

- `e2e/playwright.config.ts:44-52` binds `webServer.command` to `setupCommand()`
  (`e2e/setup-test-db.sh`).
- `e2e/setup-test-db.sh` (DROP/CREATE block, ≈ lines 36-37):
  ```bash
  $COMPOSE exec -T postgres psql -U musicops -c "DROP DATABASE IF EXISTS musicops_test;"
  $COMPOSE exec -T postgres psql -U musicops -c "CREATE DATABASE musicops_test;"
  ```

Spec-level cleanup does exist, and it is deliberate — so "cleanup is manual" is
too strong for the rows the specs own:

- `e2e/tests/artist-scan.spec.ts:30` — the reason is stated in the spec:
  > "The e2e database is recreated per CI run, but a local run reuses the stack
  > (reuseExistingServer: !CI), so this spec removes what it added rather than
  > relying on a fresh database."
  with `test.afterEach` deleting the artist (CSRF header included, per DJI-434).
- `e2e/tests/artist-picker.spec.ts:63-76` — `afterEach` cancels the queued job
  then deletes the artist, tolerating `[200, 404]`.

What remains genuinely manual is therefore not the happy path but three cases
the `afterEach` hooks structurally cannot cover: rows written by endpoints **no
spec owns cleanup for** (the `testapi` and `ga-probes.spec.ts` seeds — AGENTS.md
records "clean probe rows between runs (the spec does not delete them)"), rows
left by an **interrupted or crashed run** (an `afterEach` does not run when the
process dies), and rows another session's user owns, which read as *missing* data
rather than extra because the queries are owner-scoped (AGENTS.md).

The reason leftovers matter at all is that a later spec asserts emptiness —
`e2e/tests/artists.spec.ts` "a fresh install says nothing is monitored" — so a
leftover fails a *different* spec, one run later, with no obvious cause.

### Disposition

**Fix now, but scope it honestly.** The gap is not "no cleanup"; it is "no local
reset and no owner for seed rows". The cheapest honest fix is a documented,
explicit local-reset step (or a `--reset-db` path that re-runs the DROP/CREATE
block) plus cleanup for the `testapi`/`ga-probes` seeds — not new production
machinery inside a spec.

---

## What would falsify this verdict

- **Claim A:** adding `./internal/api/templates` to `PKGS` (or running
  `go test ./...`) and getting a green mutation cycle — i.e. that package
  passes on every mutant tree. Or a docstring that names the exclusion, which
  would make the omission a recorded decision rather than a defect.
- **Claim B:** a `-timeout` appearing in `validate.sh`/`validate.ps1`/`ci.yml`,
  or `internal/services` measured comfortably under 10m with ffmpeg installed.
- **Claim C:** `reuseExistingServer` becoming false locally, or `teardown.ts`
  dropping `-v` unconditionally instead of branching on `CI`, or every seed
  gaining an owner and a cleanup hook.

## Could not verify

- I have no terminal on this slice, so I did **not** re-run either mutation
  harness, and did not observe `internal/api/templates` failing on M8's tree
  directly. That observation is quoted from `AGENTS.md:736-737`, where it was
  measured; the package split and the missing list entry are verified from
  source.
- I did not time `internal/services`. Claim B's 9m46s figure is likewise quoted
  from `AGENTS.md:808-809`, not re-measured.
- I read only the specs named above; other specs may carry their own cleanup or
  their own leftover rows, which would make Claim C's residual list longer, not
  shorter.

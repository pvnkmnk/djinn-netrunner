# NetRunner Beta Readiness — Remaining Work Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take `master` (v0.0.1, all CI green) to the Beta Readiness project's stated acceptance criterion — a fresh clone with a filled `.env` acquires, imports into the canonical folder, tags, exposes, and streams a real Soulseek download to an external client, sessions survive a restart, and **no residual staging directories remain**.

**Architecture:** Four ordered passes, each removing a source of noise for the next: (1) make the repair tooling able to *see* case-variant fragmentation, (2) give staging cleanup a single owner so leftovers cannot accumulate, (3) repair the live library with the now-capable tooling, (4) close-out hygiene plus one final end-to-end acceptance run on a clean stack.

**Tech Stack:** Go 1.25 (Fiber, GORM, Cobra), PostgreSQL 16, Docker Compose, Playwright, HTMX/Pongo2.

**Spec:** the *NetRunner Beta Readiness* project description in Linear (`b6d8ae1b-37d5-47bc-9d41-cc63700cb94f`), which holds the ordered path this plan follows. The live record is `docs/BETA_ACCEPTANCE.md`.

## Global Constraints

- Never bump the `go` directive in `backend/go.mod` (CI pins `go 1.25.13`). Check it **last**, after any tidy.
- Run Go tests as `PORT= go test ...` — this environment exports `PORT=0`, which fails `internal/config`.
- Only gofmt files you actually edit; the repo stores CRLF and repo-wide `gofmt` floods `git status`.
- `internal/integration/*_test.go` sits behind `//go:build integration` — run `go vet -tags integration ./...` before pushing.
- The `integration` CI job can fail in ~26s with `connection reset by peer` on a Docker Hub pull (it hits docs-only commits too). Re-run; do not debug the diff.
- The live beta stack runs from a **second clone**: `projects/beta-bringup/djinn-netrunner`. Sync changed files there and rebuild `ops-worker` before any live verification, or you will be testing the previous binary.
- ffmpeg/ffprobe on the host: prefix `PATH="/c/Users/idols/scoop/shims:$PATH"`.
- Merge as `gh pr merge N --repo pvnkmnk/djinn-netrunner --squash --delete-branch`. A push 403 means the wrong `gh` account is active — check `gh auth status`, never touch remotes.
- Post PR comments with `gh api --input <file.json>` (a `gh pr comment` body containing backticks gets shell-expanded).
- Every acceptance row records commit + environment in `docs/BETA_ACCEPTANCE.md`; no row is inferred from reading code.
- Prefer editing existing files; follow the existing `docs/plans/YYYY-MM-DD-<topic>-{design,implementation-plan}.md` convention.

## File Structure

**Modify**
- `backend/internal/services/library_repair.go` — case-aware detection + case-folded merge matching
- `backend/internal/services/canonical_identity.go` — hoist the canonical-casing decision to a package-level function
- `backend/internal/services/import_file.go` — call the new discard owner instead of inline removal
- `backend/internal/api/server.go` — extract `setupRoutes` for testability (DJI-454)
- `backend/cmd/cli/main.go` — `detect-fragments` prints the canonical album casing
- `.github/workflows/e2e.yml` — trigger the default branch; call the runner script
- `docs/BETA_DEPLOYMENT.md`, `docs/RUNBOOK.md` — E2E as a manual pre-release step
- `docs/BETA_ACCEPTANCE.md`, `TODO.md`, `docs/BETA_ACCEPTANCE_EVIDENCE.md` — close-out hygiene
- `AGENTS.md` (workspace root) — this session's learnings

**Create**
- `backend/internal/services/staging_cleanup.go` — the single owner for discarding a staged download
- `backend/internal/services/library_repair_case_test.go`, `backend/internal/services/staging_cleanup_test.go`
- `backend/internal/api/server_test.go`
- `scripts/e2e.sh`, `.env.e2e.example`
- `docs/plans/2026-09-15-beta-readiness-completion-implementation-plan.md` (this plan)

---

### Task 0: Record the plan and this session's learnings

**Files:**
- Create: `docs/plans/2026-09-15-beta-readiness-completion-implementation-plan.md`
- Modify: `AGENTS.md` (workspace root, at `C:\Users\idols\DevWorks\AGENTS.md`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing code-facing; the learnings that keep later tasks from repeating mistakes

- [ ] **Step 1: Save this plan** to `docs/plans/2026-09-15-beta-readiness-completion-implementation-plan.md`, verbatim.

- [ ] **Step 2: Add four entries to the workspace `AGENTS.md`**, integrated into the existing sections (the file is **LF**, no BOM — write with `open(p, "w", newline="")` if patching by script):

Under **Ops / compose**:
```markdown
- The live beta stack runs from a *second clone* (`projects/beta-bringup/djinn-netrunner`), not the working repo — sync changed files there and rebuild `ops-worker` before live verification, or you test the previous binary and report it as evidence.
```

Under **Build / test / integration**:
```markdown
- `e2e.yml` triggers on `[main, develop]` while the default branch is `master` (every other workflow watches `[master, main]`), and its `--env-file .env.e2e` names a file that does not exist in the repo and has no example — hence zero recorded Playwright runs. "All CI green" says nothing about the browser flows.
- `DetectFragmentedAlbums` groups by *exact* folder name and `isCreditVariantSet` demands a `" & "` suffix, so case-only splits are invisible to `detect-fragments` — the repair CLI cannot see the fragments it exists to repair. `MergeAlbumFolders` matches sources with an exact `parts[1] != albumFolder` too.
```

Under **Review bots & PR workflow**:
```markdown
- The authoritative ordered dev path is the *NetRunner Beta Readiness* project description in Linear, not `TODO.md` — `TODO.md` still reads "All Cycles Completed / v0.0.1" and lists gaps that have since shipped.
```

- [ ] **Step 3: Verify** no encoding damage:
```bash
cd /c/Users/idols/DevWorks && py -c "d=open('AGENTS.md','rb').read(); print('BOM:',d[:3]==b'\xef\xbb\xbf','CRLF:',d.count(b'\r\n'))"
```
Expected: `BOM: False CRLF: 0`

- [ ] **Step 4: Commit**
```bash
cd projects/djinn-netrunner && git add docs/plans/ && git commit -m "docs: add the beta-readiness completion plan"
```

---

### Task 1: Make fragmentation detection and repair case-aware (DJI-475 prerequisite)

**Why first:** the project description orders DJI-475 before everything else, but its own caveat warns the repair CLI may not detect case-variant splits — **verified true in code**. Running the operator procedure against today's tooling would report a clean library while leaving every case-variant split in place.

**Files:**
- Modify: `backend/internal/services/library_repair.go:64-160` (detection), `:200-235` (merge matching)
- Modify: `backend/internal/services/canonical_identity.go` (hoist the canonical-casing decision)
- Modify: `backend/cmd/cli/main.go:358-420` (print the canonical album casing)
- Test: `backend/internal/services/library_repair_case_test.go` (new)

**Interfaces:**
- Consumes: `CanonicalKey(string) string` from `canonical_identity.go`
- Produces:
  - `ResolveCanonicalIdentity(db *gorm.DB, libraryRoot, artist, album string) (artist string, album string)` — package-level
  - `FragmentedAlbum.CanonicalAlbum string` — the canonical album folder casing
  - `DetectFragmentedAlbums(db, libraryRoot) ([]FragmentedAlbum, error)` — unchanged signature, wider detection
  - `MergeAlbumFolders(db, libraryRoot, albumFolder, canonicalArtist string, dryRun bool)` — unchanged signature, case-folded matching

- [ ] **Step 1: Hoist the canonical-casing decision.** In `canonical_identity.go`, add the package-level function and make the existing handler method a delegate, so the CLI and the importer cannot disagree:

```go
// ResolveCanonicalIdentity returns the canonical casing for an artist/album
// pair: the earliest acquisition's casing, else an existing on-disk folder,
// else the tag verbatim. Artist resolves before album so a new album lands in
// the artist's existing folder. Package-level so the CLI repair tooling and
// the importer share one owner for casing.
func ResolveCanonicalIdentity(db *gorm.DB, libraryRoot, artist, album string) (string, string) {
	if artist == "" || album == "" {
		return artist, album
	}
	canonicalArtist := resolveCanonicalArtist(db, libraryRoot, artist)
	return canonicalArtist, resolveCanonicalAlbum(db, libraryRoot, canonicalArtist, album)
}
```

Move the method's existing body into `resolveCanonicalArtist` / `resolveCanonicalAlbum`; leave `(*AcquisitionHandler).resolveCanonicalIdentity` calling `ResolveCanonicalIdentity(h.db, h.libraryPath(), artist, album)`.

- [ ] **Step 2: Write the failing test.**

```go
func TestDetectFragmentedAlbums_FlagsCaseOnlyAlbumSplit(t *testing.T) {
	db := newCaseTestDB(t)
	seedTrack(t, db, "PUP/The Unraveling of Puptheband/01 - Robot.m4a")
	seedTrack(t, db, "PUP/The Unraveling Of Puptheband/02 - Totally Fine.m4a")

	found, err := DetectFragmentedAlbums(db, t.TempDir())
	require.NoError(t, err)
	require.Len(t, found, 1, "a case-only album split must be detected")
	require.Equal(t, "The Unraveling of Puptheband", found[0].CanonicalAlbum)
	for _, f := range found[0].Folders {
		require.True(t, strings.EqualFold(f.AlbumFolder, found[0].CanonicalAlbum))
	}
}

func TestDetectFragmentedAlbums_FlagsCaseOnlyArtistSplit(t *testing.T) {
	db := newCaseTestDB(t)
	seedTrack(t, db, "PUP/Morbid Stuff/01.m4a")
	seedTrack(t, db, "Pup/Who Will Look After The Dogs/01.m4a")

	found, err := DetectFragmentedAlbums(db, t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, found)
	require.Equal(t, "PUP", found[0].CanonicalFolder)
}

func TestDetectFragmentedAlbums_StillIgnoresUnrelatedSameNameAlbums(t *testing.T) {
	db := newCaseTestDB(t)
	seedTrack(t, db, "Band A/Greatest Hits/01.mp3")
	seedTrack(t, db, "Band B/Greatest Hits/01.mp3")

	found, err := DetectFragmentedAlbums(db, t.TempDir())
	require.NoError(t, err)
	require.Empty(t, found, "the false-positive control must still not be flagged")
}
```

- [ ] **Step 3: Run to verify they fail**
```bash
cd projects/djinn-netrunner/backend && PORT= go test ./internal/services/ -run 'CaseOnly|UnrelatedSameName' -v
```
Expected: the two case tests FAIL (nothing detected); the control PASSES already.

- [ ] **Step 4: Implement case-aware grouping.** In `DetectFragmentedAlbums`, key the count map on the folded form and keep the actual casing:

```go
type fragmentKey struct{ artist, album string } // canonical (folded) keys
actual := map[fragmentKey]map[string]map[string]int{} // folded -> artist -> album -> count
```
Group by `CanonicalKey(parts[0])` / `CanonicalKey(parts[1])`, accumulating the *exact* names seen. Then replace the single `isCreditVariantSet` gate with a classifier:

```go
// fragmentKind classifies a multi-folder group. Credit variants are the
// original shape ("Every Time I Die" + "Every Time I Die & Daryl Palumbo");
// case variants differ only in capitalisation, which is a separate class the
// exact-name grouping could never see. Anything else (two unrelated artists
// sharing an album name) is legitimate and must not be flagged.
func fragmentKind(names []string) (credit, caseOnly bool) {
	caseOnly = true
	for _, n := range names {
		if n != names[0] {
			if CanonicalKey(n) != CanonicalKey(names[0]) {
				caseOnly = false
			}
		}
	}
	credit = isCreditVariantSetFold(names)
	return credit, caseOnly
}
```
`isCreditVariantSetFold` is the existing check with case folded on both sides. Flag a group when `len(distinct artist names) > 1` within the same folded album **and** (`credit || caseOnly`).
Set `CanonicalAlbum` / `CanonicalFolder` from `ResolveCanonicalIdentity(db, libraryRoot, sampleArtist, sampleAlbum)`; keep the existing most-tracks tie-break when resolution returns the tag verbatim. Preserve the existing `IsCanonical` marker semantics.

- [ ] **Step 5: Make the merge able to reach a case variant.** In `MergeAlbumFolders`, fold the source match and normalise the destination casing:

```go
if len(parts) != 3 || CanonicalKey(parts[1]) != CanonicalKey(albumFolder) || CanonicalKey(parts[0]) == CanonicalKey(canonicalArtist) {
	continue
}
```
and build `canonicalDir := filepath.Join(root, canonicalArtist, albumFolder)` from the **caller-supplied canonical** album casing (which the CLI will now pass from `CanonicalAlbum`). Keep the existing escape guard and the "conflicting content left in place" behaviour.

- [ ] **Step 6: Print the canonical casing** in the `detect-fragments` command, and pass `g.CanonicalAlbum` (not `g.Album`) in the printed `merge-album` invocation:
```go
fmt.Printf("    merge: netrunner-cli library merge-album <libraryID> %q %q [--apply]\n\n", g.CanonicalAlbum, g.CanonicalFolder)
```

- [ ] **Step 7: Run the tests and prove they bite**
```bash
cd projects/djinn-netrunner/backend && PORT= go test ./internal/services/ -run 'Fragmented|MergeAlbum|CaseOnly' -v
```
Expected: PASS. Then temporarily make `CanonicalKey` the identity function and confirm the two case tests go red; restore with `git checkout -- internal/services/canonical_identity.go`.

- [ ] **Step 8: Full suite + tagged vet**
```bash
cd projects/djinn-netrunner/backend && PORT= go test -count=1 ./... && go vet ./... && go vet -tags integration ./...
```

- [ ] **Step 9: Commit**
```bash
git add backend/internal/services/library_repair.go backend/internal/services/canonical_identity.go \
  backend/internal/services/library_repair_case_test.go backend/cmd/cli/main.go
git commit -m "fix: detect and repair case-only album and artist fragmentation"
```

---

### Task 2: One owner for discarding a staged download (DJI-490 + DJI-492)

**Why together:** both are the same defect in `import_file.go` — the sweep reclaims only *already-empty* directories, and the recording-ID dedup branch never removes the staged file at all. The acceptance criterion explicitly requires **no residual staging directories**, so this task closes it.

**Files:**
- Create: `backend/internal/services/staging_cleanup.go`
- Modify: `backend/internal/services/import_file.go` (steps 3.5 and 3.6, and the rejection paths)
- Modify: `backend/internal/services/job_item_processor.go` (cancel/abandon teardown)
- Test: `backend/internal/services/staging_cleanup_test.go` (new)

**Interfaces:**
- Consumes: `(*AcquisitionHandler).cleanupEmptyStagingDirs(dir string, jobID uint64, itemID *uint64)`
- Produces: `(*AcquisitionHandler).discardStagedDownload(path string, jobID uint64, itemID *uint64)`

- [ ] **Step 1: Write the failing tests.**

```go
func TestDiscardStagedDownload_RemovesFileAndSweepsEmptyDirs(t *testing.T) {
	h, root := newStagingTestHandler(t)
	album := filepath.Join(root, "ARTIST", "ALBUM")
	require.NoError(t, os.MkdirAll(album, 0o755))
	file := filepath.Join(album, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, 1, nil)

	_, err := os.Stat(file)
	require.True(t, os.IsNotExist(err), "the staged file must be removed")
	_, err = os.Stat(album)
	require.True(t, os.IsNotExist(err), "the emptied album directory must be swept")
}

func TestDiscardStagedDownload_KeepsSiblingsStillNeeded(t *testing.T) {
	h, root := newStagingTestHandler(t)
	album := filepath.Join(root, "ARTIST", "ALBUM")
	require.NoError(t, os.MkdirAll(album, 0o755))
	dup := filepath.Join(album, "01 - duplicate.m4a")
	other := filepath.Join(album, "02 - still pending.m4a")
	require.NoError(t, os.WriteFile(dup, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(other, []byte("y"), 0o644))

	h.discardStagedDownload(dup, 1, nil)

	_, err := os.Stat(dup)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(other)
	require.NoError(t, err, "a sibling another item still needs must survive")
}

func TestImportFile_RecordingIDDedupRemovesStagedFile(t *testing.T) {
	// The recording-ID branch must discard the staged file, exactly as the
	// album branch does — this is DJI-492.
	h, root := newStagingTestHandler(t)
	// ... seed an existing recording, stage a second file for the same recording ...
	importTestFile(t, h, stagedPath)
	require.NoFileExists(t, stagedPath, "the recording-ID dedup must not leave the staged file behind")
}
```

- [ ] **Step 2: Run to verify failure**
```bash
cd projects/djinn-netrunner/backend && PORT= go test ./internal/services/ -run 'DiscardStagedDownload|RecordingIDDedupRemoves' -v
```
Expected: FAIL — `discardStagedDownload` undefined.

- [ ] **Step 3: Implement the owner** in `staging_cleanup.go`:

```go
// discardStagedDownload removes a staged download that will not be imported and
// then sweeps any directories it emptied, up to (but not past) the staging root.
//
// Every path that ends an item without importing MUST go through here.
// cleanupEmptyStagingDirs alone only reclaims directories that are already
// empty, so a rejected or duplicate file kept its directory forever (DJI-490).
//
// The sibling check is deliberate: an album download is several files in one
// directory, so sweeping as soon as *this* file is gone would delete a track a
// different item still has to import. The directory goes only when nothing
// else is staged beside it.
func (h *AcquisitionHandler) discardStagedDownload(path string, jobID uint64, itemID *uint64) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("Could not remove staged download", "path", path, "error", err)
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		return
	}
	h.cleanupEmptyStagingDirs(dir, jobID, itemID)
}
```

- [ ] **Step 4: Route every non-import exit through it.** In `import_file.go`, replace the recording-ID dedup branch's bare skip with `h.discardStagedDownload(downloadPath, jobID, &itemID)`, and replace the album branch's inline remove-then-sweep with the same call. Do the same on the plausibility-rejection and ffprobe-failure paths. In `job_item_processor.go`, call it for a cancelled or abandoned item once the transfer is cancelled.

- [ ] **Step 5: Run the tests to verify they pass**
```bash
cd projects/djinn-netrunner/backend && PORT= go test ./internal/services/ -run 'DiscardStagedDownload|RecordingIDDedupRemoves|Staging|Dedup' -v
```

- [ ] **Step 6: Mutation check** — comment out the sibling guard, confirm `KeepsSiblingsStillNeeded` goes red, restore.

- [ ] **Step 7: Full suite + tagged vet, then commit**
```bash
PORT= go test -count=1 ./... && go vet ./... && go vet -tags integration ./...
git add backend/internal/services/staging_cleanup.go backend/internal/services/staging_cleanup_test.go \
  backend/internal/services/import_file.go backend/internal/services/job_item_processor.go
git commit -m "fix: reclaim staging directories a rejected or duplicate download leaves behind"
```

---

### Task 3: Repair the live beta library with the extended tooling (DJI-475)

**Files:**
- Modify: `docs/BETA_ACCEPTANCE.md` (before/after counts + evidence)
- Reference: `ops/docs/library-dedup-runbook.md`, `ops/docs/backup.md`

**Interfaces:**
- Consumes: Task 1's `detect-fragments` / `merge-album`
- Produces: a library with no fragments, recorded counts

- [ ] **Step 1: Sync and run the CLI inside the live stack.** The stack is the second clone; copy the built CLI in and run it against the real volume.
```bash
cd /c/Users/idols/DevWorks/projects/beta-bringup/djinn-netrunner
docker compose -f docker-compose.yml -f docker-compose.beta.yml exec -T ops-web netrunner-cli library detect-fragments --json
```

- [ ] **Step 2: Record the pre-repair counts** (albums, tracks, top-level artist folders) and paste the dry-run JSON into the acceptance record. **Confirm the dry run now sees the known split** — `The Unraveling Of Puptheband` beside `The Unraveling of Puptheband`, and `Pup` beside `PUP`. If it does not, stop: Task 1 is incomplete.

- [ ] **Step 3: Back up** the music library and the PostgreSQL database per `ops/docs/backup.md`; record the backup location.

- [ ] **Step 4: Confirm the false-positive control is still not flagged** — `Band A/Greatest Hits` vs `Band B/Greatest Hits`.

- [ ] **Step 5: Merge the artist splits first, dry-run then apply.** Step 2 detects `Pup` beside `PUP`, and `merge-album` cannot repair that: it only reaches tracks that already sit under the album's own folder, so an artist split whose albums do not overlap is invisible to it. The artist merge is broader — moving the artist folder takes every album under it, subsuming album-level splits it does not name.
```bash
docker compose ... exec -T ops-web netrunner-cli library merge-artist <libraryID> "<canonicalArtist>"          # dry run
docker compose ... exec -T ops-web netrunner-cli library merge-artist <libraryID> "<canonicalArtist>" --apply
```

- [ ] **Step 6: Re-run detection before touching albums.** The artist merge may have resolved splits Step 2 listed, so re-detect rather than replaying a stale plan. Merge what remains, dry-run first, then apply:
```bash
docker compose ... exec -T ops-web netrunner-cli library detect-fragments
docker compose ... exec -T ops-web netrunner-cli library merge-album <libraryID> "<canonicalAlbum>" "<canonicalArtist>"          # dry run
docker compose ... exec -T ops-web netrunner-cli library merge-album <libraryID> "<canonicalAlbum>" "<canonicalArtist>" --apply
```

- [ ] **Step 7: Verify DB and filesystem agree** — one canonical folder per album, no orphaned credit folders, no duplicate or orphaned `tracks` rows. Re-run `detect-fragments`; expect `No fragmented albums found.`

- [ ] **Step 8: Rescan** and confirm no duplicate albums or ghost entries via `getIndexes` / `getArtist`.

- [ ] **Step 9: Record before/after counts** in the runbook and as acceptance rows, then commit.

---

### Task 4: Make the E2E suite actually run, documented as a manual pre-release step (DJI-476)

**Files:**
- Modify: `.github/workflows/e2e.yml`
- Create: `scripts/e2e.sh`, `.env.e2e.example`
- Modify: `docs/BETA_DEPLOYMENT.md`, `docs/RUNBOOK.md`

**Interfaces:**
- Consumes: `docker-compose.yml` + `docker-compose.e2e.yml`
- Produces: `scripts/e2e.sh` — the one command that reproduces the suite locally

- [ ] **Step 1: Fix the trigger.** `e2e.yml` watches `[main, develop]`; the default branch is `master`. Change both `push` and `pull_request` to `[master, main]`, matching `ci.yml`/`integration.yml`.

- [ ] **Step 2: Supply the missing env file.** The workflow's `--env-file .env.e2e` names a file that does not exist and is not committed. Add `.env.e2e.example` with test-only values and commit `.env.e2e` (test-only, no real secrets), or have the script generate it from the example. Document which.

- [ ] **Step 3: Add the one-command runner** `scripts/e2e.sh`: bring up the overlay, wait for `/api/health`, run `npx playwright test`, always tear down with `down -v --remove-orphans`. Point the workflow's start/health/test/teardown steps at it so CI and a local run are the same code path.

- [ ] **Step 4: Prove it runs** — push and confirm `gh run list --workflow=e2e.yml` reports a real run with a real conclusion (currently zero runs on record). Capture the run URL as evidence.

- [ ] **Step 5: Document the spec coverage honestly.** Record in `docs/RUNBOOK.md` that the suite is a **manual pre-release step**, not a required check, and note that `artists`, `playlists`, `jobs` and `admin` specs are absent, so DJI-426/428/429/430/433/434 cannot be closed on the strength of a green suite. **Deliberately deferred** — writing those four specs is outside the beta acceptance criterion.

- [ ] **Step 6: Note the E2E status in Linear** on DJI-476 with the run URL and the manual-step decision.

- [ ] **Step 7: Commit and open a PR**; drive checks green and merge.

---

### Task 5: Close-out hygiene — roadmap docs and route tests (DJI-474 + DJI-454)

**Files:**
- Modify: `TODO.md`, `docs/BETA_ACCEPTANCE_EVIDENCE.md`
- Modify: `backend/internal/api/server.go`, Create: `backend/internal/api/server_test.go`

**Interfaces:**
- Produces: `setupRoutes(app *fiber.App, ...)` extracted so route registration is testable

- [ ] **Step 1: Refresh `TODO.md`** — it still reads "All Cycles Complete / v0.0.1" and lists as open several things that have shipped (Grafana dashboard export, E2E acceptance tests). Reconcile every line against `docs/BETA_ACCEPTANCE.md` and the closed Linear projects; move genuinely-remaining items to a clearly-marked post-v0.0.1 backlog section.

- [ ] **Step 2: Mark `docs/BETA_ACCEPTANCE_EVIDENCE.md` superseded.** It is dated 2026-05-25 / Cycle 9 / "local Go test + in-memory SQLite" and predates the acquisition hardening. Add a header pointing at `docs/BETA_ACCEPTANCE.md` as the live record, or retire the file.

- [ ] **Step 3: Extract `setupRoutes`** from the server entry point (DJI-454) so route registration can be asserted without binding a port.

- [ ] **Step 4: Write `server_test.go`** asserting the route table: a representative set of critical routes is registered, and the Subsonic routes carry the required `.view` suffix (a bare `/rest/getIndexes` must not be registered — the suffix rule reads like a missing route when it bites).
```go
func TestSetupRoutes_RegistersCriticalRoutes(t *testing.T) { /* assert table */ }
func TestSetupRoutes_SubsonicRoutesNeedViewSuffix(t *testing.T) { /* /rest/getIndexes.view present, /rest/getIndexes absent */ }
```

- [ ] **Step 5: Run the suite, commit, PR, merge**, and mark DJI-474 and DJI-454 Done with evidence comments.

---

### Task 6: Final end-to-end acceptance run on a clean stack

**Why last:** it is the project's single acceptance criterion, and every earlier task removes noise that would otherwise make a failure ambiguous.

**Files:**
- Modify: `docs/BETA_ACCEPTANCE.md`

- [ ] **Step 1: Tear down to zero volumes and bring up from a fresh clone** at the merged master commit, following only `docs/BETA_DEPLOYMENT.md`. Fix any step that needs a hand edit, an undocumented variable, or manual SQL — rather than working around it.

- [ ] **Step 2: Walk each clause of the acceptance criterion and record the observed output:**

| Clause | How it is proven |
|---|---|
| real Soulseek download acquired | worker log selecting a peer and completing the transfer |
| imported into the canonical folder | file path under `/app/music/<AlbumArtist>/<Album>/`, no credit folder |
| tagged | `ffprobe` reports `album_artist` |
| visible in the library | a `scan` job with `indexed=N failed=0` |
| playable through Subsonic by an external client | `getIndexes` → `getArtist` → `getAlbum` → `stream.view` returns real bytes, byte-identical to the library file; validate with a real `ffprobe` |
| sessions survive a restart | the same cookie answers `200` after `docker compose restart ops-web` |
| **no residual staging directories** | after the job is terminal, `/app/downloads` holds **zero** directories |

- [ ] **Step 3: Explicitly exercise the new staging path** — run one acquisition that produces a duplicate-album item and one that is cancelled, then assert `/app/downloads` is empty. Measure only after the job is terminal; an in-flight transfer legitimately holds a directory. This is the row that proves Task 2.

- [ ] **Step 4: Re-run `scripts/beta-smoke.sh`** and `scripts/e2e.sh`; record both.

- [ ] **Step 5: Fix everything the run exposes**, re-run the affected steps, and record each fix in the findings table.

- [ ] **Step 6: Full test suite** — `PORT= go test -count=1 ./...`, `go vet ./...`, `go vet -tags integration ./...`.

- [ ] **Step 7: Commit, PR, drive checks green, merge.** Sync Linear: close DJI-475/476/474/454/490/492 as Done with evidence, and set *NetRunner Beta Readiness* to Completed with the acceptance criterion quoted and the run recorded.

---

## Explicitly out of scope

Confirmed with the user: **beta acceptance criteria only**. These are tracked but not built here.

- **Subsonic `duration="0"` / empty `contentType`** (open finding in the acceptance record). Streaming is proven with real bytes and byte-identical playback, so the criterion is met without it. Deferred because concrete steps need scanner recon I did not do; it stays an open finding.
- **`artists` / `playlists` / `jobs` / `admin` Playwright specs** (DJI-476 step 3) — Task 4 documents the gap instead, so DJI-426/428/429/430/433/434 stay open honestly.
- **Post-v0.0.1 backlog** — admin panel with user management, horizontal worker scaling via LiteFS write forwarding, browser-verified mobile nav/keyboard a11y, multi-environment config files, webhook-smoke and quota-warning E2E specs. These live in `TODO.md`'s backlog section after Task 5, not in this plan.
- **Making the E2E suite a blocking required check** — the user chose the documented manual-step option.

## Verification

Each task ends independently green, and the plan is complete when:

1. `PORT= go test -count=1 ./...`, `go vet ./...`, and `go vet -tags integration ./...` are clean on master.
2. `library detect-fragments` reports no fragments against the live beta library, having first been shown to *detect* the known case-variant split.
3. `gh run list --workflow=e2e.yml` shows a real run.
4. The clean-slate run proves every clause of the acceptance criterion — including `/app/downloads` holding zero directories after a terminal job — with commit and environment recorded per row in `docs/BETA_ACCEPTANCE.md`.

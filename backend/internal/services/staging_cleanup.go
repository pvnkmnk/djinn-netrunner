package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// Staging hygiene.
//
// Every path that ends an item *without* importing must go through
// discardStagedDownload. Two defects motivated this file, and they are the same
// defect seen from two sides (DJI-490, DJI-492):
//
//   - cleanupEmptyStagingDirs reclaims only directories that are *already*
//     empty. A caller that removed the file itself and then swept was fine, but
//     a caller that never removed the file left the directory populated
//     forever; 13 of 54 staging directories survived one discography run.
//   - The MusicBrainz recording-ID dedup branch skipped the import and simply
//     returned, so its staged file stayed on disk. The album-level branch
//     (step 3.6 of importFile) already removed and swept; the two branches had
//     drifted.
//
// Routing every exit through one function is what stops them drifting again.
// StagingReclaim (staging_reclaim.go) is the other caller: the exits where the
// pipeline never learns the path at all.

// stagingRoot is the single owner of the staging-root fallback. The download
// path resolver, the yt-dlp output directory and the sweep's containment check
// all read the root from here, so they cannot disagree about where staging is —
// a disagreement is what turns an unguarded removal into a destructive one. It
// is a package function rather than a method because the slskd client and the
// acquisition handler both need it, and exactly one of them should own the
// answer.
func stagingRoot(cfg *config.Config) string {
	if cfg == nil || cfg.DownloadStagingPath == "" {
		return "./downloads"
	}
	return cfg.DownloadStagingPath
}

// withinStagingRoot reports whether path is inside the staging root, returning
// that root so a refusal can say what it was measured against. Both sides are
// made absolute first: filepath.Rel errors on a mixed absolute/relative pair,
// and bailing out on that error is exactly how the sweep came to never run at
// all under the default relative "./downloads" path. The comparison is a path
// relationship rather than a string prefix, which would also accept a sibling
// like "./downloads-backup".
func withinStagingRoot(cfg *config.Config, path string) (root string, inside bool) {
	root, err := filepath.Abs(filepath.Clean(stagingRoot(cfg)))
	if err != nil {
		return root, false
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return root, false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return root, false
	}
	return root, true
}

// isLiveItemStatus reports whether the pipeline may still be relying on the
// item's staged file. It is deliberately the inverse of a known-terminal list
// rather than a list of live statuses: a status added later must fail safe
// (keep the file) instead of authorising a delete.
func isLiveItemStatus(status string) bool {
	switch status {
	case "imported", "cancelled", "abandoned":
		return false
	}
	return !strings.HasPrefix(status, "failed") && !strings.HasPrefix(status, "completed")
}

// anotherLiveItemOwns reports whether a *different* item is still working with
// this staged path. Two items can select the same peer file — the pre-download
// gate judges format and size, never whether the file matches the track that was
// asked for — and they then resolve to the same staged path. Without this check
// the first item to finish deletes the file the second is waiting on, and the
// victim fails with "Downloaded file not found" and re-downloads it.
//
// A failed query answers "yes, someone owns it": reading a lookup failure as
// "nobody owns it" is the destructive direction, the same mistake ErrIdentityLookup
// exists to prevent for artist identity.
func (h *AcquisitionHandler) anotherLiveItemOwns(path string, itemID *uint64) bool {
	if h.db == nil {
		return false
	}

	var owners []database.JobItem
	if err := h.db.Select("id", "status").Where("download_path = ?", path).Find(&owners).Error; err != nil {
		slog.Error("Could not check staged-file ownership, leaving it in place",
			"path", path, "error", err)
		return true
	}
	for _, owner := range owners {
		if itemID != nil && owner.ID == *itemID {
			continue
		}
		if isLiveItemStatus(owner.Status) {
			return true
		}
	}
	return false
}

// The staged-path lock.
//
// Claiming a staged path (stageDownloadFile recording download_path) and
// reclaiming it (discardStagedDownload) are two workers acting on one file, and
// neither can read the other's intent from the filesystem. Two items can select
// the same peer file — the pre-download gate judges format and size, never
// whether the file matches the track that was asked for — so they resolve to the
// same staged path. Without serialisation a reclaim that has just decided "no
// live item wants this" can delete the file a second worker claimed
// microseconds later.
//
// So both sides take this lock, keyed by the path. It is the database's
// LockManager rather than an in-process mutex because the competitors are
// separate worker processes.
const (
	// stagingPathScope namespaces the key, so it cannot collide with the
	// job-scope locks the worker already takes.
	stagingPathScope = "staging_path"
	// stagingPathLockBudget bounds the wait. Reclaiming a file is never urgent:
	// declining and letting the next sweep retry costs nothing, while blocking a
	// worker behind a stall would hold up shutdown.
	stagingPathLockBudget = 2 * time.Second
	stagingPathLockPoll   = 25 * time.Millisecond
)

// absStagingPath is what the lock keys on. Both sides must derive the same key
// from the same file, and one of them may hold "./downloads/x" while the other
// holds "/app/downloads/x": a key that disagreed with itself would serialise
// nothing at all.
func absStagingPath(path string) string {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// lockStagingPath takes the cross-worker lock for one staged path. It always
// returns a release function — never nil, so a caller can defer it
// unconditionally — and whether the caller may act on the path.
//
// ok is false only when the lock could not be taken (another worker holds it, the
// database refused, or the wait expired). A reclaim must treat that as "leave the
// file alone"; a claim, whose action is the safe direction, proceeds and is
// logged.
//
// A handler with no lock manager wired reports ok: there is no second process to
// serialise with, and refusing every reclaim would turn a missing dependency into
// silently abandoned staging — the failure this file exists to prevent. See
// WithLocker for when wiring it is mandatory.
func (h *AcquisitionHandler) lockStagingPath(ctx context.Context, path string) (release func(), ok bool) {
	noop := func() {}
	if h.locker == nil {
		return noop, true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	key, err := h.locker.GetScopeLockKey(ctx, stagingPathScope, absStagingPath(path))
	if err != nil {
		slog.Error("Could not derive the staging path lock key", "path", path, "error", err)
		return noop, false
	}

	deadline := time.Now().Add(stagingPathLockBudget)
	for {
		acquired, lockErr := h.locker.AcquireTryLock(ctx, key)
		if lockErr == nil && acquired {
			return func() {
				// Released on a background context: the lock outlives a cancelled
				// caller's context, and a leaked lock would block the next sweep
				// until its expiry.
				if relErr := h.locker.ReleaseLock(context.Background(), key); relErr != nil {
					slog.Warn("Could not release the staging path lock", "path", path, "error", relErr)
				}
			}, true
		}
		if lockErr != nil {
			slog.Warn("Could not take the staging path lock", "path", path, "error", lockErr)
			return noop, false
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			slog.Warn("Timed out waiting for the staging path lock", "path", path)
			return noop, false
		}
		time.Sleep(stagingPathLockPoll)
	}
}

// logStaging records a staging decision against the item's job log, when there is
// a job to record it against. The janitor reclaims files that belong to no item,
// so it has no job; a job_id of 0 is not a job, and rows written there are read
// by nothing.
func (h *AcquisitionHandler) logStaging(jobID uint64, itemID *uint64, level, message string) {
	if h.db == nil || jobID == 0 {
		return
	}
	h.Log(jobID, level, message, itemID)
}

// discardStagedDownload removes a staged download that will not be imported and
// then sweeps any directories it emptied, up to (but not past) the staging root.
// Best-effort: a failure is logged and never propagated, because staging
// cleanup must not turn a successful import or a duplicate detection into a
// failure.
//
// It refuses any path outside the staging root, and refuses it whole. An earlier
// version guarded only the directory sweep, leaving the removal itself to trust
// its caller, so a path from a misconfigured download directory could delete a
// file the worker never staged — including one in the library. The refusal is
// deliberately loud, to the worker's stderr as well as the item's job log,
// because a misconfiguration that quietly stops cleaning up is indistinguishable
// from "nothing needed cleaning up".
//
// It also declines, quietly and by design, when another live item still owns the
// path: that is two items sharing one staged file, not a fault, and the file has
// to survive until the last of them is done with it.
//
// The ownership check and the removal happen under the staged-path lock, which
// the claim side also takes: otherwise they are two separate acts, and a worker
// that claims this path between them has the file deleted from under it. An item
// ID exempts that item from the check, and is therefore only correct for a caller
// that *is* that item's own execution — a reclaim of an item that looks terminal
// must pass none, or an item retried back into the queue would not protect its
// own file.
//
// It reports whether the file is gone as a result of the call (removed now, or
// already absent), which is what the janitor counts.
//
// The sibling check is deliberate. A whole-album download is several files
// sharing one directory, and each track is a separate job item, so sweeping as
// soon as *this* file is gone would delete a sibling that a different item still
// has to import. The directory is only reclaimed once nothing else is staged
// beside it.
func (h *AcquisitionHandler) discardStagedDownload(ctx context.Context, path string, jobID uint64, itemID *uint64) bool {
	if path == "" {
		return false
	}

	root, inside := withinStagingRoot(h.cfg, path)
	if !inside {
		slog.Warn("Refusing to discard a file outside the staging root",
			"path", path, "staging_root", root, "job_id", jobID)
		h.logStaging(jobID, itemID, "WARN",
			fmt.Sprintf("Refused to discard %s: outside the staging root %s", path, root))
		return false
	}

	// Serialised against the claim of this path; see lockStagingPath.
	unlock, ok := h.lockStagingPath(ctx, path)
	if !ok {
		slog.Warn("Leaving a staged file whose path lock could not be taken",
			"path", path, "job_id", jobID)
		h.logStaging(jobID, itemID, "WARN",
			fmt.Sprintf("Left %s in staging: another worker is working on it", path))
		return false
	}
	defer unlock()

	if h.anotherLiveItemOwns(path, itemID) {
		slog.Info("Leaving a staged file another live item still owns",
			"path", path, "job_id", jobID)
		h.logStaging(jobID, itemID, "INFO",
			fmt.Sprintf("Left %s in staging: another item is still using it", path))
		return false
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		h.logStaging(jobID, itemID, "WARN", fmt.Sprintf("Staging cleanup failed (%s): %v", path, err))
		return false
	}

	// The sweep does the sibling check for us: it reads each directory and stops
	// at the first one still holding entries, so a whole-album download keeps its
	// directory while other items are still pending. It also refuses to climb out
	// of, or remove, the staging root. Duplicating that guard here would be dead
	// code — disabling it changes no behaviour.
	h.cleanupEmptyStagingDirs(filepath.Dir(path), jobID, itemID)
	return true
}

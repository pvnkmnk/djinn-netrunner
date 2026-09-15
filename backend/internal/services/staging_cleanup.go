package services

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
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
// The sibling check is deliberate. A whole-album download is several files
// sharing one directory, and each track is a separate job item, so sweeping as
// soon as *this* file is gone would delete a sibling that a different item still
// has to import. The directory is only reclaimed once nothing else is staged
// beside it.
func (h *AcquisitionHandler) discardStagedDownload(path string, jobID uint64, itemID *uint64) {
	if path == "" {
		return
	}

	root, inside := withinStagingRoot(h.cfg, path)
	if !inside {
		slog.Warn("Refusing to discard a file outside the staging root",
			"path", path, "staging_root", root, "job_id", jobID)
		if h.db != nil {
			h.Log(jobID, "WARN", fmt.Sprintf("Refused to discard %s: outside the staging root %s", path, root), itemID)
		}
		return
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		h.Log(jobID, "WARN", fmt.Sprintf("Staging cleanup failed (%s): %v", path, err), itemID)
		return
	}

	// The sweep does the sibling check for us: it reads each directory and stops
	// at the first one still holding entries, so a whole-album download keeps its
	// directory while other items are still pending. It also refuses to climb out
	// of, or remove, the staging root. Duplicating that guard here would be dead
	// code — disabling it changes no behaviour.
	h.cleanupEmptyStagingDirs(filepath.Dir(path), jobID, itemID)
}

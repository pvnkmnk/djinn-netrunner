package services

import (
	"fmt"
	"os"
	"path/filepath"
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

// discardStagedDownload removes a staged download that will not be imported and
// then sweeps any directories it emptied, up to (but not past) the staging root.
// Best-effort: a failure is logged and never propagated, because staging
// cleanup must not turn a successful import or a duplicate detection into a
// failure.
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

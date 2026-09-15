package services

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
	"gorm.io/gorm"
)

// Reason labels for the reclaim metric.
const (
	reclaimTerminalItem = "terminal_item"
	reclaimOrphan       = "orphan"
)

// StagingReclaim is the janitor for the staging root: it reclaims staged
// downloads that no live item owns any more.
//
// The owner (discardStagedDownload) covers every exit where the pipeline itself
// learns the file is finished with. These exits cannot reach it, and they are all
// the same defect — the app never learns the path:
//
//   - a peer that begins sending *after* its transfer was abandoned (the
//     candidate loop only ever has a transfer id);
//   - a partial file a failed yt-dlp fallback left behind (DownloadAudio reports
//     a path only on success);
//   - a worker killed mid-download, which never runs its own cleanup.
//
// Recording the staged path at enqueue time (stageDownloadFile) is what makes the
// first and third of those reclaimable *by path*; the second is why the orphan
// scan exists.
//
// It reuses the owner rather than removing files itself, so the staging-root
// guard, the "another live item still owns this" check, the sibling check and the
// loud refusal all apply to everything the janitor touches — one removal path,
// not two.
type StagingReclaim struct {
	db      *gorm.DB
	handler *AcquisitionHandler
	cfg     StagingReclaimConfig
}

// StagingReclaimConfig configures the janitor.
type StagingReclaimConfig struct {
	// Enabled turns the whole janitor off. Off is a valid deployment: nothing is
	// reclaimed, and the disk shows it.
	Enabled bool
	// RemoveOrphans controls the second pass — files in staging that no item's
	// recorded path matches. It is the higher-blast-radius half, so it has its own
	// switch. With it off the janitor still reclaims terminal items' files, which
	// is precise by construction, and merely reports the orphans it sees.
	RemoveOrphans bool
	// GracePeriod is how long a file must have gone untouched before the janitor
	// will treat it as abandoned. It has to exceed the pipeline's own budget for a
	// single attempt (the 10m download timeout, plus the import) or the janitor
	// would race a transfer that is merely slow.
	GracePeriod time.Duration
	// Interval is how often to scan.
	Interval time.Duration
}

// DefaultStagingReclaimConfig returns production defaults: on, removing orphans,
// with a grace that is six times the download timeout.
func DefaultStagingReclaimConfig() StagingReclaimConfig {
	return StagingReclaimConfig{
		Enabled:       true,
		RemoveOrphans: true,
		GracePeriod:   time.Hour,
		Interval:      15 * time.Minute,
	}
}

func NewStagingReclaim(db *gorm.DB, handler *AcquisitionHandler, cfg StagingReclaimConfig) *StagingReclaim {
	return &StagingReclaim{db: db, handler: handler, cfg: cfg}
}

// ReclaimReport is what one pass did, so a run is auditable from a single log
// line and from the metric.
type ReclaimReport struct {
	// TerminalFiles is how many staged files belonged to items that are finished
	// with them and still existed.
	TerminalFiles int
	// Orphans is how many unreferenced files older than the grace were found
	// that no live item's directory protects — the ones the janitor would
	// reclaim, as opposed to the ones it keeps as siblings.
	Orphans int
	// SiblingsKept is how many of those were left because a live item expects a
	// file in the same directory — an album download still in flight.
	SiblingsKept int
	// Removed is how many files were actually reclaimed.
	Removed int
	// Deferred is how many were left in place: owned by a live item, or left
	// because orphan removal is disabled.
	Deferred int
	// BytesRemoved is the size of what was reclaimed.
	BytesRemoved int64
}

// Run scans until ctx is cancelled. A disabled janitor returns immediately rather
// than blocking: the caller wires it either way, so the switch is one deploy away
// instead of one build away.
func (s *StagingReclaim) Run(ctx context.Context, workerID string) {
	if !s.cfg.Enabled {
		slog.Info("Staging janitor disabled", "worker_id", workerID)
		return
	}

	slog.Info("Starting staging janitor", "worker_id", workerID,
		"interval", s.cfg.Interval, "grace", s.cfg.GracePeriod,
		"remove_orphans", s.cfg.RemoveOrphans)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Reclaim(ctx, workerID)
		}
	}
}

// Reclaim runs one pass. Exported so a deployment (or a test) can run it on
// demand without waiting for a tick.
func (s *StagingReclaim) Reclaim(ctx context.Context, workerID string) ReclaimReport {
	var report ReclaimReport
	if s.db == nil || s.handler == nil {
		return report
	}

	root, err := filepath.Abs(filepath.Clean(stagingRoot(s.handler.cfg)))
	if err != nil {
		slog.Error("Staging janitor could not resolve the staging root", "worker_id", workerID, "error", err)
		return report
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		// No staging root on this host: there is nothing to reclaim, and that is
		// not an error worth waking anyone for.
		return report
	}

	// Absolutised, because the walk below reports absolute paths: a stored
	// relative "./downloads/x" would never match them.
	var items []database.JobItem
	if err := s.db.Select("id", "job_id", "status", "download_path").
		Where("download_path <> ''").Find(&items).Error; err != nil {
		// A failed query must not be read as "no item owns anything": that is the
		// destructive direction for the orphan pass.
		slog.Error("Staging janitor could not list staged paths", "worker_id", workerID, "error", err)
		return report
	}

	// The directories a *live* item expects a file in. Pass two leaves those
	// whole directories alone; see the note there for why that subsumes an
	// "is this path already accounted for?" lookup.
	liveDirs := map[string]struct{}{}
	for _, item := range items {
		if !isLiveItemStatus(item.Status) {
			continue
		}
		abs, absErr := filepath.Abs(filepath.Clean(item.DownloadPath))
		if absErr != nil {
			continue
		}
		liveDirs[filepath.Dir(abs)] = struct{}{}
	}

	cutoff := time.Now().Add(-s.cfg.GracePeriod)

	// Pass 1: staged files whose item is finished with them. This is the pass
	// that reclaims an abandoned candidate's or a dead worker's download, because
	// the path was recorded before the bytes were.
	for _, item := range items {
		if ctx.Err() != nil {
			return report
		}

		info, statErr := os.Stat(item.DownloadPath)
		if statErr != nil || info.IsDir() {
			continue
		}

		if isLiveItemStatus(item.Status) {
			// Still somebody's download. Nothing to do, but a run that silently
			// ignores live files looks identical to a run that found none.
			report.Deferred++
			continue
		}

		report.TerminalFiles++
		if info.ModTime().After(cutoff) {
			continue
		}

		if s.handler.discardStagedDownload(item.DownloadPath, item.JobID, &item.ID) {
			report.Removed++
			report.BytesRemoved += info.Size()
			metrics.StagingFilesReclaimed.WithLabelValues(reclaimTerminalItem).Inc()
			continue
		}
		report.Deferred++
	}

	// Pass 2: files no item references at all — the residue of a fallback that
	// failed partway, or of anything slskd wrote that this app never asked for.
	//
	// There is deliberately no "is this file pass one's business?" lookup here. A
	// file referenced by a terminal item has already been handled above, and a
	// file whose removal pass one *deferred* is one that a live item shares — and
	// a shared path means the same directory, which puts it in liveDirs, so the
	// sibling rule below already covers it. Such a lookup would be dead code:
	// disabling it would change no behaviour.
	var orphans []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Keep walking: one unreadable entry must not abandon the sweep.
			return nil
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			// Directories are the owner's business (it sweeps the ones it empties),
			// and a symlink or device node is not something the worker staged.
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if _, ok := liveDirs[filepath.Dir(path)]; ok {
			// A live item expects a file in this directory, so the album is still
			// in flight. Leaving the whole directory alone is the same rule the
			// owner applies to siblings.
			report.SiblingsKept++
			return nil
		}

		// Counted only now: "orphan" means a file the janitor is prepared to
		// reclaim, not merely one it looked at. A file it declines to touch is a
		// sibling, and mixing the two makes the numbers useless.
		report.Orphans++
		orphans = append(orphans, path)
		return nil
	})
	if walkErr != nil {
		slog.Warn("Staging janitor walk was incomplete", "worker_id", workerID, "root", root, "error", walkErr)
	}

	if !s.cfg.RemoveOrphans {
		if len(orphans) > 0 {
			slog.Warn("Staging janitor found unreferenced files but orphan removal is disabled",
				"worker_id", workerID, "count", len(orphans), "newest_example", orphans[0])
		}
		report.Deferred += len(orphans)
	} else {
		for _, orphan := range orphans {
			if ctx.Err() != nil {
				return report
			}
			info, statErr := os.Stat(orphan)
			if statErr != nil {
				continue
			}
			// jobID 0: nothing owns this file, so there is no job log to write to.
			if s.handler.discardStagedDownload(orphan, 0, nil) {
				report.Removed++
				report.BytesRemoved += info.Size()
				metrics.StagingFilesReclaimed.WithLabelValues(reclaimOrphan).Inc()
				continue
			}
			report.Deferred++
		}
	}

	if report.Removed > 0 || report.Orphans > 0 || report.Deferred > 0 {
		slog.Info("Staging janitor finished", "worker_id", workerID,
			"terminal_files", report.TerminalFiles,
			"orphans", report.Orphans,
			"siblings_kept", report.SiblingsKept,
			"removed", report.Removed,
			"deferred", report.Deferred,
			"bytes_removed", report.BytesRemoved)
	}

	return report
}

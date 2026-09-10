package services

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
	"gorm.io/gorm"
)

// ItemResult reports what happened when a single acquisition item was processed.
type ItemResult struct {
	ItemID   uint64
	NoItems  bool  // true when no queued items remain
	ClaimErr error // non-nil if the claim itself failed
	ExecErr  error // non-nil if ExecuteItem failed
}

// JobItemProcessor handles claiming and executing individual job items
// for acquisition-type jobs. It is stateless and safe for concurrent use
// from multiple goroutines.
type JobItemProcessor struct {
	db         *gorm.DB
	acqHandler *AcquisitionHandler
}

func NewJobItemProcessor(db *gorm.DB, acqHandler *AcquisitionHandler) *JobItemProcessor {
	return &JobItemProcessor{
		db:         db,
		acqHandler: acqHandler,
	}
}

// ClaimNextItem atomically claims the next queued (or retryable) item for the
// given job. Returns 0 when no items remain.
func (p *JobItemProcessor) ClaimNextItem(jobID uint64) (uint64, error) {
	var itemID uint64
	err := p.db.Transaction(func(tx *gorm.DB) error {
		var item database.JobItem
		err := tx.Where("job_id = ? AND (status = 'queued' OR (status = 'failed' AND next_attempt_at <= ?))", jobID, time.Now()).
			Order("sequence ASC").
			First(&item).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}

		now := time.Now()
		result := tx.Model(&item).Where("status = 'queued' OR (status = 'failed' AND next_attempt_at <= ?)", now).Updates(map[string]interface{}{
			"status":     "running",
			"started_at": &now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		itemID = item.ID
		return nil
	})

	return itemID, err
}

// ProcessItem claims the next item for the job and executes it. The caller is
// responsible for calling finishJob when NoItems is true.
func (p *JobItemProcessor) ProcessItem(ctx context.Context, workerID string, jobID uint64) ItemResult {
	itemID, err := p.ClaimNextItem(jobID)
	if err != nil {
		slog.Error("Error claiming item", "worker_id", workerID, "job_id", jobID, "error", err)
		metrics.ItemsProcessedTotal.WithLabelValues("claim_error").Inc()
		return ItemResult{ClaimErr: err}
	}

	if itemID == 0 {
		return ItemResult{NoItems: true}
	}

	execErr := p.acqHandler.ExecuteItem(ctx, jobID, itemID)
	if execErr != nil {
		slog.Error("Error processing item", "worker_id", workerID, "job_id", jobID, "item_id", itemID, "error", execErr)
		metrics.ItemsProcessedTotal.WithLabelValues("error").Inc()
		// Record the failure on the item so it cannot be left 'running' forever:
		// nothing ever re-claims a running item, and an unclaimed terminal-less
		// item would keep the job from ever finalizing honestly.
		p.acqHandler.failItem(jobID, itemID, fmt.Sprintf("Item execution error: %v", execErr))
	} else {
		metrics.ItemsProcessedTotal.WithLabelValues("success").Inc()
	}
	return ItemResult{ItemID: itemID, ExecErr: execErr}
}

// AcquisitionItemStats summarizes the terminal state of every item in an
// acquisition job.
type AcquisitionItemStats struct {
	Total     int64 // every item in the job
	Succeeded int64 // imported into the library (incl. already-indexed/duplicate skips)
	Failed    int64 // permanently failed after retries (abandoned)
	Pending   int64 // queued/running/failed-with-backoff — not yet terminal
}

// acquisitionItemStats computes per-item outcome counts for a job in one query.
// Success = 'imported' or any 'completed …' status written by the import stage.
// Permanent failure = 'abandoned' (retries exhausted) or 'failed (no results)'
// (the search definitively found nothing — not retryable). Everything else
// (queued, running, downloading, retryable 'failed') is pending.
//	(COUNT(*) FILTER is PostgreSQL-only; CASE WHEN works on both backends.)
func acquisitionItemStats(db *gorm.DB, jobID uint64) AcquisitionItemStats {
	var stats AcquisitionItemStats
	db.Model(&database.JobItem{}).Where("job_id = ?", jobID).
		Select("COUNT(*) AS total, " +
			"SUM(CASE WHEN status = 'imported' OR status LIKE 'completed%' THEN 1 ELSE 0 END) AS succeeded, " +
			"SUM(CASE WHEN status = 'abandoned' OR status = 'failed (no results)' THEN 1 ELSE 0 END) AS failed, " +
			"SUM(CASE WHEN status != 'imported' AND status != 'abandoned' AND status != 'failed (no results)' AND status NOT LIKE 'completed%' THEN 1 ELSE 0 END) AS pending").
		Scan(&stats)
	return stats
}

// classifyAcquisitionOutcome maps item outcomes to an honest final job state.
//
//	succeeded — every item was acquired (or was already in the library)
//	partial   — some items acquired, some permanently failed
//	failed    — nothing was acquired
func classifyAcquisitionOutcome(s AcquisitionItemStats) (state string, summary string) {
	switch {
	case s.Total == 0:
		// Producers never create empty acquisition jobs, so this is an anomaly.
		// Failing loudly (instead of reporting success for nothing happening)
		// is the whole point of honest finalization.
		return "failed", "No items queued (anomalous: job created without items)"
	case s.Pending > 0:
		return "partial", fmt.Sprintf("%d/%d items pending retry", s.Pending, s.Total)
	case s.Succeeded == s.Total:
		return "succeeded", fmt.Sprintf("Acquired %d/%d items", s.Succeeded, s.Total)
	case s.Succeeded == 0:
		return "failed", fmt.Sprintf("Acquired 0/%d items (%d failed permanently)", s.Total, s.Failed)
	default:
		return "partial", fmt.Sprintf("Acquired %d/%d items (%d failed permanently)", s.Succeeded, s.Total, s.Failed)
	}
}

// AcquisitionFinalState computes the honest final state and summary for an
// acquisition job from its per-item outcomes. The worker calls this when an
// acquisition job has no more claimable items, so that a job whose items all
// failed to acquire is never reported as "succeeded".
func AcquisitionFinalState(db *gorm.DB, jobID uint64) (state string, summary string) {
	return classifyAcquisitionOutcome(acquisitionItemStats(db, jobID))
}

// RunSafely wraps fn with panic recovery. If the function panics, the panic is
// caught, logged, and returned as an error.
func RunSafely(workerID string, jobID uint64, jobType string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			slog.Error("job goroutine panicked",
				"worker_id", workerID,
				"job_id", jobID,
				"job_type", jobType,
				"panic", r,
				"stack", string(stack),
			)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

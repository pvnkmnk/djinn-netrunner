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
	} else {
		metrics.ItemsProcessedTotal.WithLabelValues("success").Inc()
	}
	return ItemResult{ItemID: itemID, ExecErr: execErr}
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

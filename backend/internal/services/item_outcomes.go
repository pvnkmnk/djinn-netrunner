package services

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// Item outcomes: the single home of "this attempt ended, here is what happens
// next" for a job item. Every terminal or retryable verdict the acquisition
// pipeline records goes through one of the three writers below, so a change to
// retry accounting lands in exactly one place.
//
// The rule the trio encodes:
//
//   - failItem is "this attempt failed, and the retry machinery owns the rest":
//     it schedules a backoff retry, or abandonment once the job's attempt limit
//     is reached.
//   - abandonItem is a verdict the pipeline has already proved permanent — a
//     gate rejection, a refused destination. It never schedules a retry, because
//     the same work would reach the same conclusion.
//   - noResultsItem is the definitive answer for this attempt cycle: a search
//     that returned nothing is not a transient failure, and cycling the item
//     would spam the peer network with pointless searches.

// noResultsItem records a terminal "nothing was found" outcome for an item.
// Unlike failItem, it does not schedule retries: a search that returned no
// results is a definitive answer for this attempt cycle, and pretending it is
// a transient failure would both spam the peer network with pointless
// searches and (previously) leave items cycling instead of finalizing.
func (h *AcquisitionHandler) noResultsItem(jobID uint64, itemID uint64, reason string) {
	h.Log(jobID, "ERR", reason, &itemID)
	h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":         "failed (no results)",
		"failure_reason": reason,
		"finished_at":    time.Now(),
	})
}

func (h *AcquisitionHandler) failItem(jobID uint64, itemID uint64, reason string) {
	h.Log(jobID, "ERR", reason, &itemID)

	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		slog.Error("Failed to find item for failure update", "job_id", jobID, "item_id", itemID, "error", err)
		return
	}

	// Check job-level max attempts to determine if item should be abandoned
	var job database.Job
	abandoned := false
	if err := h.db.First(&job, jobID).Error; err == nil {
		maxAttempts := job.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3 // safety default
		}
		if item.RetryCount+1 >= maxAttempts {
			abandoned = true
		}
	}

	if abandoned {
		slog.Warn("Item exceeded max retries, abandoning", "job_id", jobID, "item_id", itemID, "retries", item.RetryCount+1)
		h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
			"status":         "abandoned",
			"failure_reason": reason,
			"retry_count":    item.RetryCount + 1,
			"finished_at":    time.Now(),
		})
		return
	}

	backoff := database.CalculateBackoff(item.RetryCount)
	nextAttempt := time.Now().Add(backoff)

	h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":          "failed",
		"failure_reason":  reason,
		"retry_count":     item.RetryCount + 1,
		"next_attempt_at": &nextAttempt,
		"finished_at":     time.Now(),
	})
}

// abandonItem records a verdict the pipeline can prove is permanent, so the
// item is finished on the first attempt instead of being scheduled for retries
// that would repeat the same work and reach the same conclusion. `abandoned` is
// the status for that: ClaimNextItem takes only queued items and failed ones
// whose backoff has passed, and the item accounting already counts it as
// permanently failed.
// It returns the error when the verdict cannot be written, so a caller cannot
// report success for a terminal state that was never recorded.
func (h *AcquisitionHandler) abandonItem(jobID uint64, itemID uint64, reason string) error {
	h.Log(jobID, "ERR", reason, &itemID)

	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		slog.Error("Failed to find item for abandonment", "job_id", jobID, "item_id", itemID, "error", err)
		return fmt.Errorf("load item %d to abandon it: %w", itemID, err)
	}
	slog.Warn("Item abandoned on a permanent verdict",
		"job_id", jobID, "item_id", itemID, "attempt", item.RetryCount+1, "reason", reason)
	if err := h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":          "abandoned",
		"failure_reason":  reason,
		"retry_count":     item.RetryCount + 1,
		"next_attempt_at": nil,
		"finished_at":     time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("record the abandonment of item %d: %w", itemID, err)
	}
	return nil
}

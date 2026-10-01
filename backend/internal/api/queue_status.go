package api

import (
	"fmt"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// JobQueueStatus explains, at render time, why a queued job has not started.
//
// A "queued" badge on its own was the whole of what the Jobs page said, which
// left an operator unable to tell a job that is merely waiting its turn from
// one that is never going to run — and the second kind is what a wedged queue
// looks like from the outside. Two different things hold a queued job back and
// they have to read differently, because only one of them is something the
// operator can do anything about:
//
//   - Another job holds this job's scope. That job is named, because it is on
//     the same page and can itself be cleared.
//   - Nothing else. The worker simply has not got to it yet.
//
// Positions come from the whole queue ordered oldest first, which is the order
// the worker claims in. A filtered or limited view must not renumber the queue,
// so this reads every queued job rather than the subset the page happens to
// show.
type JobQueueStatus struct {
	Position int
	Reason   string
}

// queueStatus returns the position and reason for every queued job, keyed by job
// ID. A job that is not queued has no entry; callers only ask for these on
// queued rows.
//
// The scope comparison deliberately mirrors the worker's claimCandidates
// filter, which matches queued against running on (scope_type, scope_id).
// Anything that makes the worker skip a job has to be something this explains —
// otherwise the page would report a job as merely "waiting" while the worker was
// never going to pick it up.
func queueStatus(db *gorm.DB) (map[uint64]JobQueueStatus, error) {
	statuses := make(map[uint64]JobQueueStatus)

	var queued []database.Job
	if err := db.Select("id, job_type, scope_type, scope_id").
		Where("state = ?", "queued").
		Order("requested_at ASC").
		Find(&queued).Error; err != nil {
		return nil, err
	}
	if len(queued) == 0 {
		return statuses, nil
	}

	// scope -> the running job holding it. A worker takes an advisory lock for
	// the scope of every job it runs, and claimCandidates will not hand out a
	// queued job whose scope is already running.
	holders := make(map[string]database.Job)
	var running []database.Job
	if err := db.Select("id, job_type, scope_type, scope_id").
		Where("state = ?", "running").
		Find(&running).Error; err != nil {
		return nil, err
	}
	for _, job := range running {
		holders[scopeKey(job.ScopeType, job.ScopeID)] = job
	}

	for i, job := range queued {
		status := JobQueueStatus{Position: i + 1}
		if holder, blocked := holders[scopeKey(job.ScopeType, job.ScopeID)]; blocked {
			status.Reason = fmt.Sprintf("Waiting on job #%d (%s), which holds the same %s scope",
				holder.ID, holder.Type, scopeLabel(job.ScopeType))
		} else {
			status.Reason = fmt.Sprintf("Waiting for a worker slot (%d running)", len(running))
		}
		statuses[job.ID] = status
	}
	return statuses, nil
}

// scopeKey mirrors the (scope_type, scope_id) pair claimCandidates compares in
// SQL: two jobs share a key exactly when the worker will refuse to start the
// second one because the first is already running.
//
// A job with no scope — the scheduler's release_monitor rows — shares the empty
// key with every other scopeless job, which is also what the worker's SQL does,
// because scope_type = scope_type holds for two empty strings. That is correct
// rather than a coincidence: two scopeless system jobs really do contend for
// the same slot.
func scopeKey(scopeType, scopeID string) string {
	return scopeType + "\x00" + scopeID
}

// scopeLabel names a scope for use in a sentence. Scopeless system jobs get a
// word rather than an empty string, because "the same  scope" reads as a bug.
func scopeLabel(scopeType string) string {
	if scopeType == "" {
		return "system"
	}
	return scopeType
}
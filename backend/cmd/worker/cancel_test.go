package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerActiveJob puts a job row in the DB and the same job in the
// orchestrator's active set, the way claimAndProcess leaves them.
func registerActiveJob(t *testing.T, w *WorkerOrchestrator, job *database.Job, state string, processing bool) *jobContext {
	t.Helper()
	job.State = state
	job.RequestedAt = time.Now()
	require.NoError(t, w.db.Create(job).Error)

	ctx, cancel := context.WithCancel(context.Background())
	jc := &jobContext{job: *job, ctx: ctx, cancel: cancel, lockKey: 0, processing: processing}
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = jc
	w.jobMutex.Unlock()
	return jc
}

// A cancel request writes 'cancelled' straight to the row. Nothing else
// re-reads it, so a job sitting idle in the active set must be torn down on
// the next round-robin tick — otherwise the endpoint is a no-op.
func TestProcessActiveJobsRoundRobin_CancelledJobStops(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "sync"}, "cancelled", false)

	w.processActiveJobsRoundRobin()

	w.jobMutex.Lock()
	_, stillActive := w.activeJobs[jc.job.ID]
	w.jobMutex.Unlock()
	assert.False(t, stillActive, "a cancelled job must leave the active set")

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State, "the state the request wrote must survive")
	assert.Equal(t, "Cancelled by request", job.Summary)
	require.NotNil(t, job.FinishedAt, "a cancelled job is finished, not eternally running")
}

// While a goroutine is mid-item the round-robin must abort it without removing
// the job itself: that goroutine owns the teardown, and releasing the scope
// lock out from under it would let a second job enter the same scope.
func TestProcessActiveJobsRoundRobin_CancelledJobInFlightAborts(t *testing.T) {
	w := setupWorkerTestDB(t)
	// An unsupported type guarantees that if the loop had spawned the worker
	// goroutine, finishJob would have overwritten the state with a failure.
	jc := registerActiveJob(t, w, &database.Job{Type: "unsupported_type"}, "cancelled", true)

	w.processActiveJobsRoundRobin()

	select {
	case <-jc.ctx.Done():
	default:
		t.Fatal("the in-flight job's context must be cancelled so the work actually stops")
	}

	w.jobMutex.Lock()
	_, stillActive := w.activeJobs[jc.job.ID]
	w.jobMutex.Unlock()
	assert.True(t, stillActive, "the running goroutine owns the teardown, not the loop")

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State, "no work may be spawned for a cancelled job")
}

// finishJob runs when the job's own goroutine wraps up. A cancel that lands
// during that window must survive, or a user-visible cancel silently becomes
// "succeeded".
func TestFinishJob_PreservesCancelledState(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "acquisition"}, "cancelled", true)

	w.finishJob(jc.job.ID, nil)
	w.wg.Wait()

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State)
	assert.Equal(t, "Cancelled by request", job.Summary)
	assert.Empty(t, job.ErrorDetail)
}

// The abort we cause surfaces as a context error. Reporting a cancelled job as
// failed would blame the user's own cancel request.
func TestFinishJob_CancelWinsOverAbortError(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "acquisition"}, "cancelled", true)

	w.finishJob(jc.job.ID, errors.New("context canceled"))

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State)
	assert.Empty(t, job.ErrorDetail, "the abort must not be recorded as a job error")
}

// Cancelled items are terminal: a cancelled acquisition must not claim items
// are "pending retry" when nothing will ever retry them.
func TestFinishJob_CancelledAcquisitionIsNotReportedAsPartial(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "acquisition"}, "cancelled", true)

	items := []database.JobItem{
		{JobID: jc.job.ID, TrackTitle: "a", Status: "cancelled"},
		{JobID: jc.job.ID, TrackTitle: "b", Status: "cancelled"},
		{JobID: jc.job.ID, TrackTitle: "c", Status: "imported"},
	}
	for i := range items {
		require.NoError(t, w.db.Create(&items[i]).Error)
	}

	w.finishJob(jc.job.ID, nil)
	w.wg.Wait()

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State)
	assert.NotContains(t, job.Summary, "pending retry")
}

// Tearing down a cancelled job must not leave items claimed under a job nobody
// is working on — nothing else ever revisits 'running'/'downloading' items.
func TestFinishCancelledJob_CancelsClaimedItems(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "acquisition"}, "cancelled", false)

	retryAt := time.Now().Add(time.Minute)
	items := []database.JobItem{
		{JobID: jc.job.ID, TrackTitle: "queued one", Status: "queued"},
		{JobID: jc.job.ID, TrackTitle: "downloading one", Status: "downloading"},
		{JobID: jc.job.ID, TrackTitle: "retry pending", Status: "failed", NextAttemptAt: &retryAt},
		{JobID: jc.job.ID, TrackTitle: "permanently failed", Status: "failed"},
		{JobID: jc.job.ID, TrackTitle: "already done", Status: "imported"},
	}
	for i := range items {
		require.NoError(t, w.db.Create(&items[i]).Error)
	}

	w.finishCancelledJob(jc)
	w.wg.Wait()

	w.jobMutex.Lock()
	_, stillActive := w.activeJobs[jc.job.ID]
	w.jobMutex.Unlock()
	assert.False(t, stillActive)

	var queued database.JobItem
	require.NoError(t, w.db.First(&queued, "track_title = ?", "queued one").Error)
	assert.Equal(t, "cancelled", queued.Status)

	var downloading database.JobItem
	require.NoError(t, w.db.First(&downloading, "track_title = ?", "downloading one").Error)
	assert.Equal(t, "cancelled", downloading.Status)

	// A failed item awaiting a retry is pending work. Once the job is finished
	// nothing can claim it, so it must not keep advertising a scheduled attempt.
	var retryPending database.JobItem
	require.NoError(t, w.db.First(&retryPending, "track_title = ?", "retry pending").Error)
	assert.Equal(t, "cancelled", retryPending.Status)
	assert.Nil(t, retryPending.NextAttemptAt, "a scheduled retry that cannot run must be cleared")

	// A genuine failure with no retry scheduled is a real outcome, not a cancel.
	var failed database.JobItem
	require.NoError(t, w.db.First(&failed, "track_title = ?", "permanently failed").Error)
	assert.Equal(t, "failed", failed.Status, "a real failure keeps its outcome")

	var imported database.JobItem
	require.NoError(t, w.db.First(&imported, "track_title = ?", "already done").Error)
	assert.Equal(t, "imported", imported.Status, "items already imported keep their outcome")

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Equal(t, "cancelled", job.State)
	require.NotNil(t, job.FinishedAt)
}

// A cancelled acquisition is a second finalizer, and it has to do the same
// post-acquisition work: it may already have imported tracks, and leaving them
// out of the index is how the cancel path hid them (the clean-slate run caught
// a cancellation that finalized without queueing any scan at all).
func TestFinishCancelledJob_QueuesScanForAcquisition(t *testing.T) {
	w := setupWorkerTestDB(t)
	w.cfg.MusicLibraryPath = t.TempDir()
	lib := database.Library{Name: "Music", Path: w.cfg.MusicLibraryPath}
	require.NoError(t, w.db.Create(&lib).Error)

	jc := registerActiveJob(t, w, &database.Job{Type: "acquisition"}, "cancelled", false)
	require.NoError(t, w.db.Create(&database.JobItem{
		JobID: jc.job.ID, TrackTitle: "imported before the cancel", Status: "imported",
	}).Error)

	w.finishCancelledJob(jc)
	w.wg.Wait()
	w.wg.Wait()

	var scans []database.Job
	require.NoError(t, w.db.Where("job_type = ?", "scan").Find(&scans).Error)
	require.Len(t, scans, 1, "a cancelled acquisition must still refresh the index")
	assert.Equal(t, lib.ID.String(), scans[0].ScopeID)
}

// A cancelled job that imported nothing has no reason to trigger a scan.
func TestFinishCancelledJob_NoScanForNonAcquisition(t *testing.T) {
	w := setupWorkerTestDB(t)
	w.cfg.MusicLibraryPath = t.TempDir()
	require.NoError(t, w.db.Create(&database.Library{Name: "Music", Path: w.cfg.MusicLibraryPath}).Error)

	jc := registerActiveJob(t, w, &database.Job{Type: "sync"}, "cancelled", false)

	w.finishCancelledJob(jc)
	w.wg.Wait()
	w.wg.Wait()

	var count int64
	require.NoError(t, w.db.Model(&database.Job{}).Where("job_type = ?", "scan").Count(&count).Error)
	assert.EqualValues(t, 0, count)
}

// A cancel request writes the state directly. If no worker owns the job - the
// common case after a worker restart, and the case the clean-slate run hit -
// no job goroutine ever runs the teardown, so the row would stay 'cancelled'
// with no finished_at or summary forever.
func TestFinalizeOrphanedCancellations_StampsJobsNoWorkerOwns(t *testing.T) {
	w := setupWorkerTestDB(t)
	w.cfg.MusicLibraryPath = t.TempDir()
	lib := database.Library{Name: "Music", Path: w.cfg.MusicLibraryPath}
	require.NoError(t, w.db.Create(&lib).Error)

	orphan := database.Job{Type: "acquisition", State: "cancelled", RequestedAt: time.Now()}
	require.NoError(t, w.db.Create(&orphan).Error)
	retryAt := time.Now().Add(time.Minute)
	require.NoError(t, w.db.Create(&database.JobItem{
		JobID: orphan.ID, TrackTitle: "still claimed", Status: "downloading",
	}).Error)
	require.NoError(t, w.db.Create(&database.JobItem{
		JobID: orphan.ID, TrackTitle: "retry pending", Status: "failed", NextAttemptAt: &retryAt,
	}).Error)

	w.finalizeOrphanedCancellations()
	w.wg.Wait()

	var job database.Job
	require.NoError(t, w.db.First(&job, orphan.ID).Error)
	require.NotNil(t, job.FinishedAt, "an orphaned cancellation must still be finished")
	assert.Equal(t, "Cancelled by request", job.Summary)

	var claimed database.JobItem
	require.NoError(t, w.db.First(&claimed, "track_title = ?", "still claimed").Error)
	assert.Equal(t, "cancelled", claimed.Status)

	var pending database.JobItem
	require.NoError(t, w.db.First(&pending, "track_title = ?", "retry pending").Error)
	assert.Equal(t, "cancelled", pending.Status)
	assert.Nil(t, pending.NextAttemptAt)

	var scans []database.Job
	require.NoError(t, w.db.Where("job_type = ?", "scan").Find(&scans).Error)
	assert.Len(t, scans, 1, "an orphaned cancelled acquisition still refreshes the index")
}

// A job this worker is actively tearing down must be left to its own goroutine.
func TestFinalizeOrphanedCancellations_LeavesActiveJobsAlone(t *testing.T) {
	w := setupWorkerTestDB(t)
	jc := registerActiveJob(t, w, &database.Job{Type: "sync"}, "cancelled", true)

	w.finalizeOrphanedCancellations()

	var job database.Job
	require.NoError(t, w.db.First(&job, jc.job.ID).Error)
	assert.Nil(t, job.FinishedAt, "the owning goroutine finalizes it, not the janitor")
}

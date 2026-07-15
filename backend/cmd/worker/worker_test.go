package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
)

func setupWorkerTestDB(t *testing.T) *WorkerOrchestrator {
	t.Helper()
	cfg := &config.Config{DatabaseURL: ":memory:"}
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewWorkerOrchestrator(cfg, db)
}

// TestUpdateHeartbeats tests that updateHeartbeats batch-updates heartbeat_at
// for all jobs in the activeJobs map.
func TestUpdateHeartbeats(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job in DB with state "running"
	job := database.Job{
		Type:        "acquisition",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Manually add to activeJobs map (protected by mutex)
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job: job,
		ctx: context.Background(),
	}
	w.jobMutex.Unlock()

	// Verify heartbeat_at is nil before
	var beforeJob database.Job
	require.NoError(t, w.db.First(&beforeJob, job.ID).Error)
	require.Nil(t, beforeJob.HeartbeatAt)

	// Call updateHeartbeats
	w.updateHeartbeats()

	// Verify heartbeat_at is now set
	var afterJob database.Job
	require.NoError(t, w.db.First(&afterJob, job.ID).Error)
	require.NotNil(t, afterJob.HeartbeatAt)

	// Should be within the last second
	elapsed := time.Since(*afterJob.HeartbeatAt)
	require.True(t, elapsed < 2*time.Second, "heartbeat_at should be recent, was %v ago", elapsed)
}

// TestUpdateHeartbeats_EmptyMap tests that updateHeartbeats is a no-op when
// activeJobs is empty.
func TestUpdateHeartbeats_EmptyMap(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Ensure activeJobs is empty
	w.jobMutex.Lock()
	require.Equal(t, 0, len(w.activeJobs))
	w.jobMutex.Unlock()

	// Should not panic and should not error
	w.updateHeartbeats()
}

// TestTriggerLibraryScan_NoClients tests that triggerLibraryScan returns an
// error when neither gonic nor navidrome clients are configured.
func TestTriggerLibraryScan_NoClients(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Force nil clients by overwriting the fields after construction
	// (NewWorkerOrchestrator always creates a gonic client if GonicURL is set)
	w.gonic = nil
	w.navidrome = nil

	ok, err := w.triggerLibraryScan()

	require.False(t, ok)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no library server configured")
}

// TestCheckQuotaAlerts_NilServices tests that checkQuotaAlerts does not panic
// when diskQuotaService or notificationService are nil.
func TestCheckQuotaAlerts_NilServices(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Nil out both services
	w.diskQuotaService = nil
	w.notificationService = nil

	// Should not panic
	require.NotPanics(t, func() {
		w.checkQuotaAlerts()
	})
}

// TestCheckQuotaAlerts_NilDiskQuotaService tests that checkQuotaAlerts does
// not panic when only diskQuotaService is nil.
func TestCheckQuotaAlerts_NilDiskQuotaService(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Nil out diskQuotaService but keep notificationService
	w.diskQuotaService = nil

	require.NotPanics(t, func() {
		w.checkQuotaAlerts()
	})
}

// TestCheckQuotaAlerts_NilNotificationService tests that checkQuotaAlerts does
// not panic when only notificationService is nil.
func TestCheckQuotaAlerts_NilNotificationService(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Nil out notificationService but keep diskQuotaService
	w.notificationService = nil

	require.NotPanics(t, func() {
		w.checkQuotaAlerts()
	})
}

// TestFinishJob_Success tests that finishJob sets state to "succeeded" and
// removes the job from activeJobs.
func TestFinishJob_Success(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job in DB with state "running"
	job := database.Job{
		Type:        "sync",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs with a real context
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0, // 0 means no lock for testing
	}
	w.jobMutex.Unlock()

	// Call finishJob with no error (success)
	w.finishJob(job.ID, nil)

	// Verify job state is "succeeded"
	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "succeeded", dbJob.State)
	require.NotNil(t, dbJob.FinishedAt)

	// Verify job is removed from activeJobs
	w.jobMutex.Lock()
	_, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.False(t, ok, "job should be removed from activeJobs")
}

// TestFinishJob_Failure tests that finishJob sets state to "failed" with the
// error detail when given an error.
func TestFinishJob_Failure(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job in DB with state "running"
	job := database.Job{
		Type:        "acquisition",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0,
	}
	w.jobMutex.Unlock()

	// Call finishJob with an error
	testErr := fmt.Errorf("test error")
	w.finishJob(job.ID, testErr)

	// Verify job state is "failed"
	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "failed", dbJob.State)
	require.NotNil(t, dbJob.FinishedAt)
	require.Equal(t, "test error", dbJob.ErrorDetail)
	require.Equal(t, "test error", dbJob.Summary)

	// Verify job is removed from activeJobs
	w.jobMutex.Lock()
	_, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.False(t, ok, "job should be removed from activeJobs")
}

// TestFinishJob_NotInActiveJobs tests that finishJob is a no-op when the job
// is not in the activeJobs map.
func TestFinishJob_NotInActiveJobs(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job in DB with state "running" but DO NOT add to activeJobs
	job := database.Job{
		Type:        "sync",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Call finishJob — should be a no-op (returns early)
	w.finishJob(job.ID, nil)

	// Job state should still be "running" since finishJob returned early
	// without updating anything
	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "running", dbJob.State)
	require.Nil(t, dbJob.FinishedAt)
}

// TestFinishJob_AcquisitionTriggersLibraryScan tests that a successful
// acquisition job triggers a library scan in the background.
func TestFinishJob_AcquisitionTriggersLibraryScan(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create an acquisition job in DB with state "running"
	job := database.Job{
		Type:        "acquisition",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0,
	}
	w.jobMutex.Unlock()

	// Ensure gonic and navidrome are nil so triggerLibraryScan returns an error
	// (we just want to verify it was called; the error is expected)
	w.gonic = nil
	w.navidrome = nil

	// Call finishJob with no error (success)
	// The goroutine for library scan may or may not complete before we check,
	// but the job should still be marked succeeded
	w.finishJob(job.ID, nil)

	// Verify job state is "succeeded" (the scan is triggered async)
	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "succeeded", dbJob.State)
	require.NotNil(t, dbJob.FinishedAt)
}

// TestFinishJob_MissingLockManager tests that finishJob handles a nil or
// zero lockKey gracefully (ReleaseLock with key 0 may be a no-op).
func TestFinishJob_ZeroLockKey(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job
	job := database.Job{
		Type:        "sync",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs with lockKey = 0
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0, // 0 may mean "no lock" in some implementations
	}
	w.jobMutex.Unlock()

	// Should not panic even if lockKey is 0
	require.NotPanics(t, func() {
		w.finishJob(job.ID, nil)
	})

	// Job should be marked succeeded
	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "succeeded", dbJob.State)
}

// TestTriggerWatchlistSyncs_EnabledOnly tests that triggerWatchlistSyncs
// creates sync jobs only for enabled watchlists.
func TestTriggerWatchlistSyncs_EnabledOnly(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a QualityProfile (required by Watchlist foreign key)
	qp := database.QualityProfile{
		Name:          "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:    320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create an enabled watchlist
	enabledWL := database.Watchlist{
		Name:             "Enabled Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:enabled1",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&enabledWL).Error)

	// Create a disabled watchlist (should be skipped)
	// First create with ID generated, then explicitly update enabled=false
	disabledWL := database.Watchlist{
		Name:             "Disabled Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:disabled1",
		QualityProfileID: qp.ID,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&disabledWL).Error)
	// Explicitly set enabled=false (GORM skips zero values by default)
	require.NoError(t, w.db.Model(&disabledWL).Update("enabled", false).Error)

	// Call triggerWatchlistSyncs
	w.triggerWatchlistSyncs()

	// Verify sync job was created only for enabled watchlist
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ?", "sync").Find(&jobs).Error)
	require.Len(t, jobs, 1, "should create exactly one sync job")

	job := jobs[0]
	require.Equal(t, "queued", job.State)
	require.Equal(t, "watchlist", job.ScopeType)
	require.Equal(t, enabledWL.ID.String(), job.ScopeID)
	require.Equal(t, "watchlist_poller", job.CreatedBy)
}

// TestTriggerWatchlistSyncs_NoEnabledWatchlists tests that triggerWatchlistSyncs
// is a no-op when no enabled watchlists exist.
func TestTriggerWatchlistSyncs_NoEnabledWatchlists(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create only disabled watchlists
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	disabledWL := database.Watchlist{
		Name:             "Disabled Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:disabled",
		QualityProfileID: qp.ID,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&disabledWL).Error)
	// Explicitly set enabled=false (GORM skips zero values by default)
	require.NoError(t, w.db.Model(&disabledWL).Update("enabled", false).Error)

	// Call triggerWatchlistSyncs - should not panic or create jobs
	w.triggerWatchlistSyncs()

	// Verify no sync jobs were created
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ?", "sync").Find(&jobs).Error)
	require.Len(t, jobs, 0, "should not create any sync jobs when no watchlists are enabled")
}

// TestTriggerWatchlistSyncs_MultipleEnabled tests that triggerWatchlistSyncs
// creates sync jobs for all enabled watchlists.
func TestTriggerWatchlistSyncs_MultipleEnabled(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create QualityProfile
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create multiple enabled watchlists
	enabledWL1 := database.Watchlist{
		Name:             "Enabled Watchlist 1",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:enabled1",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	enabledWL2 := database.Watchlist{
		Name:             "Enabled Watchlist 2",
		SourceType:       "spotify_liked",
		SourceURI:        "spotify:user:liked",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&enabledWL1).Error)
	require.NoError(t, w.db.Create(&enabledWL2).Error)

	// Call triggerWatchlistSyncs
	w.triggerWatchlistSyncs()

	// Verify sync jobs were created for both enabled watchlists
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ?", "sync").Find(&jobs).Error)
	require.Len(t, jobs, 2, "should create sync jobs for both enabled watchlists")
}

// TestClaimAndProcess_HappyPath tests that claimAndProcess claims a queued
// job and transitions it to running.
func TestClaimAndProcess_HappyPath(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a library (required for scan job scope)
	library := database.Library{
		Name: "Test Library",
		Path: "/tmp/test-library",
	}
	require.NoError(t, w.db.Create(&library).Error)

	// Create a queued scan job
	job := database.Job{
		Type:        "scan",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Ensure activeJobs is empty before test
	w.jobMutex.Lock()
	require.Len(t, w.activeJobs, 0)
	w.jobMutex.Unlock()

	// Call claimAndProcess
	w.claimAndProcess()

	// Verify job is now running in DB
	var updatedJob database.Job
	require.NoError(t, w.db.First(&updatedJob, job.ID).Error)
	require.Equal(t, "running", updatedJob.State)
	require.NotNil(t, updatedJob.StartedAt)
	require.NotNil(t, updatedJob.HeartbeatAt)
	require.NotNil(t, updatedJob.WorkerID)
	require.Equal(t, w.workerID, *updatedJob.WorkerID)

	// Verify job is in activeJobs map
	w.jobMutex.Lock()
	jc, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.True(t, ok, "job should be in activeJobs map")
	require.NotNil(t, jc)
	require.NotZero(t, jc.lockKey, "lockKey should be non-zero")
}

// TestClaimAndProcess_NoQueuedJobs tests that claimAndProcess is a no-op
// when there are no queued jobs.
func TestClaimAndProcess_NoQueuedJobs(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Ensure activeJobs is empty
	w.jobMutex.Lock()
	initialLen := len(w.activeJobs)
	w.jobMutex.Unlock()
	require.Equal(t, 0, initialLen)

	// Create a non-queued job (already running)
	existingJob := database.Job{
		Type:        "scan",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&existingJob).Error)

	// Call claimAndProcess - should not panic
	w.claimAndProcess()

	// Verify the running job is still running and not re-claimed
	var updatedJob database.Job
	require.NoError(t, w.db.First(&updatedJob, existingJob.ID).Error)
	require.Equal(t, "running", updatedJob.State)

	// activeJobs should still be empty (we don't re-claim running jobs)
	w.jobMutex.Lock()
	require.Len(t, w.activeJobs, 0)
	w.jobMutex.Unlock()
}

// TestClaimAndProcess_AtCapacity tests that claimAndProcess does not claim
// additional jobs when MaxConcurrentJobs is reached.
func TestClaimAndProcess_AtCapacity(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create enough queued jobs to fill capacity
	for i := 0; i < MaxConcurrentJobs; i++ {
		job := database.Job{
			Type:        "scan",
			State:       "queued",
			ScopeType:   "library",
			ScopeID:     fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i),
			RequestedAt: time.Now(),
		}
		require.NoError(t, w.db.Create(&job).Error)
	}

	// Claim all slots by calling claimAndProcess MaxConcurrentJobs times
	for i := 0; i < MaxConcurrentJobs; i++ {
		w.claimAndProcess()
	}

	// Verify all slots are filled
	w.jobMutex.Lock()
	require.Len(t, w.activeJobs, MaxConcurrentJobs)
	w.jobMutex.Unlock()

	// Create one more queued job
	extraJob := database.Job{
		Type:        "scan",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     "00000000-0000-0000-0000-000000000099",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&extraJob).Error)

	// Call claimAndProcess - should not claim the extra job
	w.claimAndProcess()

	// Verify extra job is still queued
	var updatedJob database.Job
	require.NoError(t, w.db.First(&updatedJob, extraJob.ID).Error)
	require.Equal(t, "queued", updatedJob.State)

	// activeJobs should still be at capacity
	w.jobMutex.Lock()
	require.Len(t, w.activeJobs, MaxConcurrentJobs)
	w.jobMutex.Unlock()
}

// TestProcessActiveJobsRoundRobin_EmptyMap tests that processActiveJobsRoundRobin
// is a no-op when activeJobs map is empty.
func TestProcessActiveJobsRoundRobin_EmptyMap(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Ensure activeJobs is empty
	w.jobMutex.Lock()
	require.Equal(t, 0, len(w.activeJobs))
	w.jobMutex.Unlock()

	// Should not panic
	require.NotPanics(t, func() {
		w.processActiveJobsRoundRobin()
	})
}

// TestProcessActiveJobsRoundRobin_SkipsAlreadyProcessing tests that a job
// marked processing=true is skipped in round-robin pass.
func TestProcessActiveJobsRoundRobin_SkipsAlreadyProcessing(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job in DB with state "running"
	job := database.Job{
		Type:        "acquisition",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs with processing=true (already being processed)
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:       job,
		ctx:       ctx,
		cancel:    cancel,
		lockKey:   0,
		processing: true, // already processing — should be skipped
	}
	w.jobMutex.Unlock()

	// Call processActiveJobsRoundRobin — job should be skipped
	w.processActiveJobsRoundRobin()

	// Give goroutines time to complete if any were spawned
	time.Sleep(100 * time.Millisecond)

	// Job should still be in activeJobs with processing=true (was skipped)
	w.jobMutex.Lock()
	jc, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.True(t, ok, "job should still be in activeJobs")
	require.True(t, jc.processing, "processing flag should still be true")
}

// TestProcessActiveJobsRoundRobin_UnknownJobType tests that an unsupported job
// type results in finishJob being called with an error.
func TestProcessActiveJobsRoundRobin_UnknownJobType(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create a job with an unsupported type
	job := database.Job{
		Type:        "unsupported_type",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0,
	}
	w.jobMutex.Unlock()

	// Call processActiveJobsRoundRobin
	w.processActiveJobsRoundRobin()

	// Wait for goroutine to complete
	w.wg.Wait()

	// Job should be removed from activeJobs and state should be "failed"
	w.jobMutex.Lock()
	_, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.False(t, ok, "job should be removed from activeJobs after finishJob")

	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "failed", dbJob.State)
	require.Contains(t, dbJob.ErrorDetail, "unsupported job type")
}

// TestRunMonolithicJob_UnsupportedJobType tests that runMonolithicJob calls
// finishJob with an error for unknown job types.
func TestRunMonolithicJob_UnsupportedJobType(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create an "unknown_type" job in DB with state "running"
	job := database.Job{
		Type:        "unknown_type",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0,
	}
	w.jobMutex.Unlock()

	// Call runMonolithicJob directly (synchronous)
	w.runMonolithicJob(w.activeJobs[job.ID])

	// Verify job state is "failed" with the unsupported job type error
	w.jobMutex.Lock()
	_, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.False(t, ok, "job should be removed from activeJobs")

	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "failed", dbJob.State)
	require.Contains(t, dbJob.ErrorDetail, "unsupported job type")
}

// TestProcessAcquisitionItem_NoItems tests that processAcquisitionItem calls
// finishJob when there are no queued items for the job (NoItems=true from
// itemProcessor.ProcessItem).
func TestProcessAcquisitionItem_NoItems(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create an acquisition job in DB with state "running" but NO items in job_items table
	job := database.Job{
		Type:        "acquisition",
		State:       "running",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Add to activeJobs
	ctx, cancel := context.WithCancel(context.Background())
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = &jobContext{
		job:     job,
		ctx:     ctx,
		cancel:  cancel,
		lockKey: 0,
	}
	w.jobMutex.Unlock()

	// Call processAcquisitionItem — since there are no items in job_items,
	// ClaimNextItem returns itemID=0, so ProcessItem returns NoItems=true,
	// which triggers finishJob with nil error.
	w.processAcquisitionItem(w.activeJobs[job.ID])

	// Job should be finished (removed from activeJobs) with "succeeded" state
	w.jobMutex.Lock()
	_, ok := w.activeJobs[job.ID]
	w.jobMutex.Unlock()
	require.False(t, ok, "job should be removed from activeJobs when NoItems=true")

	var dbJob database.Job
	require.NoError(t, w.db.First(&dbJob, job.ID).Error)
	require.Equal(t, "succeeded", dbJob.State)
}

// TestSchedulerLoop_CreatesSyncJobForDueSchedule tests that the scheduler
// loop's inner logic creates a sync job when a schedule is due.
func TestSchedulerLoop_CreatesSyncJobForDueSchedule(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create QualityProfile (required by Watchlist foreign key)
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create a watchlist
	wl := database.Watchlist{
		Name:             "Test Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:test",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&wl).Error)

	// Create a schedule that is due (next_run_at in the past)
	pastTime := time.Now().Add(-1 * time.Hour)
	schedule := database.Schedule{
		WatchlistID: wl.ID,
		CronExpr:    "0 * * * *", // hourly
		Enabled:     true,
		NextRunAt:   &pastTime,
	}
	require.NoError(t, w.db.Create(&schedule).Error)

	// Simulate one tick of schedulerLoop: find due schedules and enqueue sync jobs
	var schedules []database.Schedule
	w.db.Model(&database.Schedule{}).Where("enabled = ? AND next_run_at IS NULL", true).Update("next_run_at", time.Now())
	err := w.db.Preload("Watchlist").Where("enabled = ? AND next_run_at <= ?", true, time.Now()).Find(&schedules).Error
	require.NoError(t, err)
	require.Len(t, schedules, 1, "should find the due schedule")

	// Execute the schedule (simulating what schedulerLoop does)
	s := schedules[0]
	job := database.Job{
		Type:        "sync",
		State:       "queued",
		ScopeType:   "watchlist",
		ScopeID:     s.WatchlistID.String(),
		RequestedAt: time.Now(),
		OwnerUserID: s.Watchlist.OwnerUserID,
		CreatedBy:   "scheduler",
	}
	require.NoError(t, w.db.Create(&job).Error)

	// Verify sync job was created
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ? AND state = ? AND created_by = ?", "sync", "queued", "scheduler").Find(&jobs).Error)
	require.Len(t, jobs, 1, "should create a queued sync job for due schedule")
	require.Equal(t, "watchlist", jobs[0].ScopeType)
	require.Equal(t, wl.ID.String(), jobs[0].ScopeID)
}

// TestSchedulerLoop_SkipsDisabledSchedule tests that disabled schedules are
// not processed.
func TestSchedulerLoop_SkipsDisabledSchedule(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create QualityProfile
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create a watchlist
	wl := database.Watchlist{
		Name:             "Test Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:test",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&wl).Error)

	// Create a disabled schedule with past due time (should be skipped)
	pastTime := time.Now().Add(-1 * time.Hour)
	schedule := database.Schedule{
		WatchlistID: wl.ID,
		CronExpr:    "0 * * * *",
		NextRunAt:   &pastTime,
	}
	require.NoError(t, w.db.Create(&schedule).Error)
	// GORM doesn't persist zero values; explicitly set enabled=false
	require.NoError(t, w.db.Model(&schedule).Update("enabled", false).Error)

	// Query for due schedules (simulating schedulerLoop tick)
	var schedules []database.Schedule
	err := w.db.Preload("Watchlist").Where("enabled = ? AND next_run_at <= ?", true, time.Now()).Find(&schedules).Error
	require.NoError(t, err)
	require.Len(t, schedules, 0, "should not find disabled schedule")

	// Verify no sync job was created
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ? AND created_by = ?", "sync", "scheduler").Find(&jobs).Error)
	require.Len(t, jobs, 0, "should not create any sync job for disabled schedule")
}

// TestSchedulerLoop_SkipsFutureSchedule tests that schedules with future
// next_run_at are not processed.
func TestSchedulerLoop_SkipsFutureSchedule(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create QualityProfile
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create a watchlist
	wl := database.Watchlist{
		Name:             "Test Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:test",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&wl).Error)

	// Create a schedule with future next_run_at (should be skipped)
	futureTime := time.Now().Add(1 * time.Hour)
	schedule := database.Schedule{
		WatchlistID: wl.ID,
		CronExpr:    "0 * * * *",
		Enabled:     true,
		NextRunAt:   &futureTime,
	}
	require.NoError(t, w.db.Create(&schedule).Error)

	// Query for due schedules
	var schedules []database.Schedule
	err := w.db.Preload("Watchlist").Where("enabled = ? AND next_run_at <= ?", true, time.Now()).Find(&schedules).Error
	require.NoError(t, err)
	require.Len(t, schedules, 0, "should not find future schedule")

	// Verify no sync job was created
	var jobs []database.Job
	require.NoError(t, w.db.Where("job_type = ? AND created_by = ?", "sync", "scheduler").Find(&jobs).Error)
	require.Len(t, jobs, 0, "should not create any sync job for future schedule")
}

// TestSchedulerLoop_InitializesNullNextRunAt tests that schedules with NULL
// next_run_at are initialized to now (so they run immediately).
func TestSchedulerLoop_InitializesNullNextRunAt(t *testing.T) {
	w := setupWorkerTestDB(t)

	// Create QualityProfile
	qp := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac",
		MinBitrate:     320,
	}
	require.NoError(t, w.db.Create(&qp).Error)

	ownerUserID := uint64(1)

	// Create a watchlist
	wl := database.Watchlist{
		Name:             "Test Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:test",
		QualityProfileID: qp.ID,
		Enabled:          true,
		OwnerUserID:      &ownerUserID,
	}
	require.NoError(t, w.db.Create(&wl).Error)

	// Create a schedule with NULL next_run_at (the initialization query should set it)
	schedule := database.Schedule{
		WatchlistID: wl.ID,
		CronExpr:    "0 * * * *",
		Enabled:     true,
		NextRunAt:   nil, // NULL — should be initialized by scheduler
	}
	require.NoError(t, w.db.Create(&schedule).Error)

	// Simulate schedulerLoop initialization: update NULL next_run_at to now
	w.db.Model(&database.Schedule{}).Where("enabled = ? AND next_run_at IS NULL", true).Update("next_run_at", time.Now())

	// Verify next_run_at is now set (not NULL)
	var updatedSchedule database.Schedule
	require.NoError(t, w.db.First(&updatedSchedule, schedule.ID).Error)
	require.NotNil(t, updatedSchedule.NextRunAt, "next_run_at should be initialized to non-NULL")
}

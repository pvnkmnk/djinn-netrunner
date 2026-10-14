package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A schedule names the job type it runs, and the job type names the scope:
// sync a watchlist, scan or enrich a library. These tests drive
// `runDueSchedules` -- the tick the loop calls -- rather than re-implementing
// its body, which is what the earlier scheduler tests did: they built their own
// job from the fixture and could not have caught a bug in the loop they were
// supposed to cover.

type scheduledJob struct {
	jobType   string
	scopeType string
	scopeID   string
}

func scheduledJobs(t *testing.T, w *WorkerOrchestrator) []scheduledJob {
	t.Helper()
	var jobs []database.Job
	require.NoError(t, w.db.Where("created_by = ?", "scheduler").Order("id").Find(&jobs).Error)
	got := make([]scheduledJob, 0, len(jobs))
	for _, job := range jobs {
		got = append(got, scheduledJob{job.Type, job.ScopeType, job.ScopeID})
	}
	return got
}

// TestRunDueSchedules_QueuesTheScopeEachJobTypeNames covers all three
// schedulable job types in one tick, because the mapping between a job type and
// its scope is the thing that changed: a library schedule that queued a
// watchlist-scoped job would run against nothing at all.
func TestRunDueSchedules_QueuesTheScopeEachJobTypeNames(t *testing.T) {
	w := setupWorkerTestDB(t)

	owner := uint64(7)
	user := database.User{ID: owner, Email: "scheduler@example.test", PasswordHash: "x", Role: "user"}
	require.NoError(t, w.db.Create(&user).Error)

	profile := database.QualityProfile{Name: "Scheduler Scope", AllowedFormats: "flac", MinBitrate: 320}
	require.NoError(t, w.db.Create(&profile).Error)
	watchlist := database.Watchlist{
		Name: "Scheduled Watchlist", SourceType: "spotify_playlist", SourceURI: "spotify:playlist:scheduled",
		QualityProfileID: profile.ID, Enabled: true, OwnerUserID: &owner,
	}
	require.NoError(t, w.db.Create(&watchlist).Error)
	library := database.Library{Name: "Scheduled Library", Path: filepath.Clean(t.TempDir()), OwnerUserID: &owner}
	require.NoError(t, w.db.Create(&library).Error)

	due := time.Now().Add(-time.Hour)
	schedules := []database.Schedule{
		{JobType: "sync", WatchlistID: &watchlist.ID, CronExpr: "0 1 * * *", Timezone: "UTC", Enabled: true, NextRunAt: &due},
		{JobType: "scan", LibraryID: &library.ID, CronExpr: "0 2 * * *", Timezone: "UTC", Enabled: true, NextRunAt: &due},
		{JobType: "enrich", LibraryID: &library.ID, CronExpr: "0 3 * * *", Timezone: "UTC", Enabled: true, NextRunAt: &due},
	}
	for i := range schedules {
		require.NoError(t, w.db.Create(&schedules[i]).Error)
	}

	w.runDueSchedules()

	assert.ElementsMatch(t, []scheduledJob{
		{"sync", "watchlist", watchlist.ID.String()},
		{"scan", "library", library.ID.String()},
		{"enrich", "library", library.ID.String()},
	}, scheduledJobs(t, w))

	// The jobs belong to the account that owns the scope, not to the scheduler.
	var queued []database.Job
	require.NoError(t, w.db.Where("created_by = ?", "scheduler").Find(&queued).Error)
	require.Len(t, queued, 3)
	for _, job := range queued {
		require.NotNil(t, job.OwnerUserID, "%s job has no owner", job.Type)
		assert.Equal(t, owner, *job.OwnerUserID)
	}

	// Each schedule was moved past the tick that ran it, so the next tick does
	// not queue a second copy of work that is already queued.
	for _, schedule := range schedules {
		var after database.Schedule
		require.NoError(t, w.db.First(&after, schedule.ID).Error)
		require.NotNil(t, after.NextRunAt)
		assert.True(t, after.NextRunAt.After(time.Now()), "schedule %d is still due", schedule.ID)
		require.NotNil(t, after.LastRunAt)
	}

	w.runDueSchedules()
	assert.Len(t, scheduledJobs(t, w), 3, "a schedule that already ran must not queue twice")
}

// TestRunDueSchedules_DisablesAScheduleThatNamesNoScope covers the rows the
// loop has to refuse: a job type it does not dispatch, and a schedulable job
// type with the scope column empty. Queueing either would create a job with an
// empty scope id, and every empty-scope job contends on one advisory lock --
// which is what leaves the queue looking hung. Retrying it every thirty seconds
// forever is the other way to get this wrong, so the schedule is disabled with
// the reason in the log.
func TestRunDueSchedules_DisablesAScheduleThatNamesNoScope(t *testing.T) {
	w := setupWorkerTestDB(t)

	due := time.Now().Add(-time.Hour)
	broken := []database.Schedule{
		{JobType: "acquisition", CronExpr: "0 6 * * *", Timezone: "UTC", Enabled: true, NextRunAt: &due},
		{JobType: "scan", CronExpr: "0 7 * * *", Timezone: "UTC", Enabled: true, NextRunAt: &due},
	}
	for i := range broken {
		require.NoError(t, w.db.Create(&broken[i]).Error)
	}

	w.runDueSchedules()

	assert.Empty(t, scheduledJobs(t, w), "a schedule that names no scope must not queue anything")

	for _, schedule := range broken {
		var after database.Schedule
		require.NoError(t, w.db.First(&after, schedule.ID).Error)
		assert.False(t, after.Enabled, "schedule %d should have been disabled rather than retried", schedule.ID)
	}
}

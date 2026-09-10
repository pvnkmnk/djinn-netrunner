package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnqueueMonitorJobIfIdle verifies the dedup/idempotence semantics of the
// recurring release-monitor job: seeded at startup, skipped while a previous
// one is still queued/running, and re-created after it finishes — so multiple
// workers and restarts cannot stack duplicate monitor jobs.
func TestEnqueueMonitorJobIfIdle(t *testing.T) {
	db := setupTestDB(t)

	require.True(t, EnqueueMonitorJobIfIdle(db), "first enqueue must succeed")
	var jobs []Job
	require.NoError(t, db.Where("job_type = ?", "release_monitor").Find(&jobs).Error)
	require.Len(t, jobs, 1)
	require.Equal(t, "queued", jobs[0].State)
	require.Equal(t, "scheduler", jobs[0].CreatedBy)

	// Queued monitor job present → skip.
	require.False(t, EnqueueMonitorJobIfIdle(db), "must not stack while queued")

	// Running monitor job present → still skip.
	require.NoError(t, db.Model(&jobs[0]).Update("state", "running").Error)
	require.False(t, EnqueueMonitorJobIfIdle(db), "must not stack while running")

	// Terminal → new job is created.
	require.NoError(t, db.Model(&jobs[0]).Update("state", "partial").Error)
	require.True(t, EnqueueMonitorJobIfIdle(db), "terminal previous job allows a new one")
	require.NoError(t, db.Where("job_type = ?", "release_monitor").Find(&jobs).Error)
	require.Len(t, jobs, 2)
}

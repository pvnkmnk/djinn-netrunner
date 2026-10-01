package api

import (
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The position a queued job reports is its rank in the queue the worker claims
// from — oldest first — so "3rd" means the same thing to the operator as it does
// to the worker.
func TestQueueStatus_PositionIsRankOldestFirst(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	oldest := time.Now().Add(-30 * time.Minute)
	middle := time.Now().Add(-20 * time.Minute)
	newest := time.Now().Add(-10 * time.Minute)

	// Inserted newest first on purpose: the assertion is about rank, not about
	// what the insert order happened to be.
	for _, requested := range []time.Time{newest, oldest, middle} {
		job := database.Job{Type: "scan", State: "queued", RequestedAt: requested}
		require.NoError(t, db.Create(&job).Error)
	}

	statuses, err := queueStatus(db)
	require.NoError(t, err)
	require.Len(t, statuses, 3)

	var rows []database.Job
	require.NoError(t, db.Where("state = ?", "queued").Order("requested_at ASC").Find(&rows).Error)
	require.Len(t, rows, 3)

	assert.Equal(t, 1, statuses[rows[0].ID].Position)
	assert.Equal(t, 2, statuses[rows[1].ID].Position)
	assert.Equal(t, 3, statuses[rows[2].ID].Position)
}

// Positions must be recomputed per render, not baked in: as the queue drains the
// job behind it moves up. A stale "position 7" is worse than no position at all.
func TestQueueStatus_PositionsAdvanceAsTheQueueDrains(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	first := database.Job{Type: "scan", State: "queued", RequestedAt: time.Now().Add(-30 * time.Minute)}
	second := database.Job{Type: "sync", State: "queued", RequestedAt: time.Now().Add(-20 * time.Minute)}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)

	statuses, err := queueStatus(db)
	require.NoError(t, err)
	assert.Equal(t, 1, statuses[first.ID].Position)
	assert.Equal(t, 2, statuses[second.ID].Position)

	// The head of the queue is claimed and starts running.
	require.NoError(t, db.Model(&database.Job{}).Where("id = ?", first.ID).
		Update("state", "running").Error)

	statuses, err = queueStatus(db)
	require.NoError(t, err)
	assert.Equal(t, 1, statuses[second.ID].Position,
		"the job behind must move up when the one ahead leaves the queue")
}

// A queued job whose scope is held by a running one is not "waiting its turn" —
// it will not start until that job finishes or is cleared, which is a different
// thing for an operator to do about. The reason has to name the blocker.
func TestQueueStatus_ReasonNamesTheJobHoldingTheScope(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	running := database.Job{
		Type: "acquisition", State: "running",
		ScopeType: "artist", ScopeID: "artist-1",
		RequestedAt: time.Now(),
	}
	require.NoError(t, db.Create(&running).Error)

	blocked := database.Job{
		Type: "artist_scan", State: "queued",
		ScopeType: "artist", ScopeID: "artist-1",
		RequestedAt: time.Now(),
	}
	require.NoError(t, db.Create(&blocked).Error)

	statuses, err := queueStatus(db)
	require.NoError(t, err)

	reason := statuses[blocked.ID].Reason
	assert.Contains(t, reason, "job #")
	assert.Contains(t, reason, "acquisition", "the reason must name the job that is blocking it")
	assert.Contains(t, reason, "artist", "the reason must say which scope is contended")
}

// A queued job with nothing in its way is simply waiting for the worker. Saying
// so plainly matters: before this the page said nothing at all, and an operator
// could not tell this case from the wedged one above.
func TestQueueStatus_UnblockedJobWaitsForASlot(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	running := database.Job{
		Type: "acquisition", State: "running",
		ScopeType: "artist", ScopeID: "artist-1",
		RequestedAt: time.Now(),
	}
	require.NoError(t, db.Create(&running).Error)

	waiting := database.Job{
		Type: "scan", State: "queued",
		ScopeType: "library", ScopeID: "library-1",
		RequestedAt: time.Now(),
	}
	require.NoError(t, db.Create(&waiting).Error)

	statuses, err := queueStatus(db)
	require.NoError(t, err)
	assert.Contains(t, statuses[waiting.ID].Reason, "worker slot")
	assert.NotContains(t, statuses[waiting.ID].Reason, "job #", "nothing is blocking this one")
}

// The worker's claim query skips queued jobs whose scope is running, matching on
// scope_type and scope_id. If this explanation disagreed with that filter the
// page would report a job as merely waiting while the worker was never going to
// pick it up — so two scopeless jobs contend here exactly as they do there.
func TestQueueStatus_ScopelessSystemJobsContend(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	// The scheduler's release_monitor rows carry no scope at all.
	running := database.Job{Type: "release_monitor", State: "running", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&running).Error)

	queued := database.Job{Type: "release_monitor", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&queued).Error)

	statuses, err := queueStatus(db)
	require.NoError(t, err)
	assert.Contains(t, statuses[queued.ID].Reason, "release_monitor")
	assert.Contains(t, statuses[queued.ID].Reason, "system",
		"a scopeless job needs a word where the scope would be, not an empty gap")
}

// Only queued rows get a position and a reason. A running or finished job has no
// place in the queue, and offering one would be a second, wrong answer.
func TestQueueStatus_NonQueuedJobsGetNoEntry(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	for _, state := range []string{"running", "succeeded", "failed", "partial", "cancelled"} {
		job := database.Job{Type: "scan", State: state, RequestedAt: time.Now()}
		require.NoError(t, db.Create(&job).Error)
	}

	statuses, err := queueStatus(db)
	require.NoError(t, err)
	assert.Empty(t, statuses)
}
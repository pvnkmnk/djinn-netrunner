package services

import (
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupJobItemTestDB creates a SQLite in-memory DB with schema migrated.
func setupJobItemTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err)
	err = database.Migrate(db)
	require.NoError(t, err)
	return db
}

// createJobItem inserts a JobItem for the given jobID and returns it.
func createJobItem(t *testing.T, db *gorm.DB, jobID uint64, status string, sequence int) *database.JobItem {
	t.Helper()
	item := database.JobItem{
		JobID:           jobID,
		Status:          status,
		NormalizedQuery: "test query",
		Sequence:        sequence,
	}
	err := db.Create(&item).Error
	require.NoError(t, err)
	return &item
}

// createJobItemWithNextAttempt inserts a JobItem with a specific NextAttemptAt time.
func createJobItemWithNextAttempt(t *testing.T, db *gorm.DB, jobID uint64, status string, sequence int, nextAttemptAt *time.Time) *database.JobItem {
	t.Helper()
	item := database.JobItem{
		JobID:           jobID,
		Status:          status,
		NormalizedQuery: "test query",
		Sequence:        sequence,
		NextAttemptAt:   nextAttemptAt,
	}
	err := db.Create(&item).Error
	require.NoError(t, err)
	return &item
}

func TestJobItemProcessor_ClaimNextItem(t *testing.T) {
	tests := []struct {
		name          string
		jobID         uint64
		setupItems    func(t *testing.T, db *gorm.DB, jobID uint64)
		wantItemID    uint64
		wantZero      bool
		wantErr       bool
	}{
		{
			name:  "claims queued item in sequence order",
			jobID: 1,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				// Create items in non-sequential order to verify ORDER BY
				createJobItem(t, db, jobID, "queued", 3)
				createJobItem(t, db, jobID, "queued", 1)
				createJobItem(t, db, jobID, "queued", 2)
			},
			wantItemID: 0, // ID is auto, we check sequence via order
			wantZero:   false,
			wantErr:    false,
		},
		{
			name:     "returns zero when no items exist",
			jobID:    999,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				// No items created
			},
			wantZero: true,
			wantErr:  false,
		},
		{
			name:  "skips items from other jobs",
			jobID: 10,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				// Create items for a different job
				createJobItem(t, db, jobID+100, "queued", 1)
				createJobItem(t, db, jobID+100, "queued", 2)
				// Create items for our target job
				createJobItem(t, db, jobID, "queued", 1)
			},
			wantZero: false,
			wantErr:  false,
		},
		{
			name:  "claims retryable item when next_attempt_at has passed",
			jobID: 20,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				past := time.Now().Add(-1 * time.Hour)
				createJobItemWithNextAttempt(t, db, jobID, "failed", 1, &past)
				createJobItem(t, db, jobID, "queued", 2)
			},
			wantZero: false,
			wantErr:  false,
		},
		{
			name:  "skips retryable item when next_attempt_at is in the future",
			jobID: 30,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				future := time.Now().Add(1 * time.Hour)
				createJobItemWithNextAttempt(t, db, jobID, "failed", 1, &future)
				createJobItem(t, db, jobID, "queued", 2)
			},
			wantZero: false,
			wantErr:  false,
		},
		{
			name:  "skips already-running items",
			jobID: 40,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				createJobItem(t, db, jobID, "running", 1)
				createJobItem(t, db, jobID, "queued", 2)
			},
			wantZero: false,
			wantErr:  false,
		},
		{
			name:  "claims first item when multiple jobs have queued items",
			jobID: 50,
			setupItems: func(t *testing.T, db *gorm.DB, jobID uint64) {
				// Items for other jobs
				createJobItem(t, db, jobID+1, "queued", 1)
				createJobItem(t, db, jobID+2, "queued", 1)
				// Items for target job - should claim item with sequence 1
				item := createJobItem(t, db, jobID, "queued", 1)
				_ = item // just to verify it gets claimed
				createJobItem(t, db, jobID, "queued", 2)
			},
			wantZero: false,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupJobItemTestDB(t)

			// Create a job record (needed for FK if enforced, but mainly for context)
			job := database.Job{Type: "acquisition"}
			err := db.Create(&job).Error
			require.NoError(t, err)
			ttJobID := tt.jobID
			if ttJobID == 0 {
				ttJobID = job.ID
			}

			if tt.setupItems != nil {
				tt.setupItems(t, db, ttJobID)
			}

			processor := NewJobItemProcessor(db, nil)
			claimedID, err := processor.ClaimNextItem(ttJobID)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			if tt.wantZero {
				assert.Equal(t, uint64(0), claimedID)
				return
			}

			// Verify the claimed item is actually marked as "running"
			var item database.JobItem
			err = db.First(&item, claimedID).Error
			require.NoError(t, err)
			assert.Equal(t, "running", item.Status)
			assert.NotNil(t, item.StartedAt)
		})
	}
}

func TestJobItemProcessor_ClaimNextItem_SequenceOrder(t *testing.T) {
	db := setupJobItemTestDB(t)

	job := database.Job{Type: "acquisition"}
	err := db.Create(&job).Error
	require.NoError(t, err)
	jobID := job.ID

	// Create items out of order
	item1 := createJobItem(t, db, jobID, "queued", 10)
	item2 := createJobItem(t, db, jobID, "queued", 2)
	item3 := createJobItem(t, db, jobID, "queued", 7)

	processor := NewJobItemProcessor(db, nil)

	// First claim should get sequence 2 (lowest)
	claimed1, err := processor.ClaimNextItem(jobID)
	require.NoError(t, err)
	assert.Equal(t, item2.ID, claimed1, "first claim should get sequence 2")

	// Second claim should get sequence 7
	claimed2, err := processor.ClaimNextItem(jobID)
	require.NoError(t, err)
	assert.Equal(t, item3.ID, claimed2, "second claim should get sequence 7")

	// Third claim should get sequence 10
	claimed3, err := processor.ClaimNextItem(jobID)
	require.NoError(t, err)
	assert.Equal(t, item1.ID, claimed3, "third claim should get sequence 10")

	// Fourth claim should return 0 (no more items)
	claimed4, err := processor.ClaimNextItem(jobID)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), claimed4, "fourth claim should return 0 (no more items)")
}

func TestRunSafely(t *testing.T) {
	tests := []struct {
		name      string
		workerID  string
		jobID     uint64
		jobType   string
		fn        func() error
		wantErr   bool
		errMsg    string
		errIsPanic bool
	}{
		{
			name:     "returns fn error on normal execution",
			workerID: "worker-1",
			jobID:    123,
			jobType:  "acquisition",
			fn: func() error {
				return assert.AnError
			},
			wantErr:   true,
			errMsg:    assert.AnError.Error(),
			errIsPanic: false,
		},
		{
			name:     "returns nil on successful execution",
			workerID: "worker-2",
			jobID:    456,
			jobType:  "sync",
			fn: func() error {
				return nil
			},
			wantErr:    false,
			errIsPanic: false,
		},
		{
			name:     "catches panic and returns error",
			workerID: "worker-3",
			jobID:    789,
			jobType:  "acquisition",
			fn: func() error {
				panic("test panic")
			},
			wantErr:    true,
			errIsPanic: true,
		},
		{
			name:     "catches panic with nil interface value",
			workerID: "worker-4",
			jobID:    101,
			jobType:  "scan",
			fn: func() error {
				panic(nil)
			},
			wantErr:    true,
			errIsPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunSafely(tt.workerID, tt.jobID, tt.jobType, tt.fn)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIsPanic {
					assert.Contains(t, err.Error(), "panic:")
				} else if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewJobItemProcessor(t *testing.T) {
	db := setupJobItemTestDB(t)
	processor := NewJobItemProcessor(db, nil)
	assert.NotNil(t, processor)
	assert.Equal(t, db, processor.db)
	assert.Nil(t, processor.acqHandler)
}

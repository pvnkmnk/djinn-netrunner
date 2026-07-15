package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err, "failed to connect to db")
	err = database.Migrate(db)
	require.NoError(t, err, "failed to migrate db")
	sqlDB, err := db.DB()
	require.NoError(t, err, "failed to get underlying sql.DB")
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func TestAcquisitionHandler_FailItem(t *testing.T) {
	// 1. Setup DB
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// 2. Create job and item
	job := database.Job{Type: "acquisition"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// 3. Fail item (1st time)
	handler.failItem(job.ID, item.ID, "test failure")

	// 4. Verify
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")

	if updatedItem.Status != "failed" {
		t.Errorf("expected status 'failed', got %s", updatedItem.Status)
	}
	if updatedItem.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %d", updatedItem.RetryCount)
	}
	if updatedItem.NextAttemptAt == nil {
		t.Error("expected NextAttemptAt to be set")
	} else {
		// Backoff for 0 retries is 1 minute
		expectedTime := time.Now().Add(1 * time.Minute)
		if updatedItem.NextAttemptAt.Before(expectedTime.Add(-10*time.Second)) || updatedItem.NextAttemptAt.After(expectedTime.Add(10*time.Second)) {
			t.Errorf("expected NextAttemptAt around %v, got %v", expectedTime, updatedItem.NextAttemptAt)
		}
	}

	// 5. Fail item again (2nd time)
	handler.failItem(job.ID, item.ID, "test failure 2")
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")

	if updatedItem.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %d", updatedItem.RetryCount)
	}

	// Backoff for 1 retry is 5 minutes
	expectedTime := time.Now().Add(5 * time.Minute)
	if updatedItem.NextAttemptAt.Before(expectedTime.Add(-10*time.Second)) || updatedItem.NextAttemptAt.After(expectedTime.Add(10*time.Second)) {
		t.Errorf("expected NextAttemptAt around %v, got %v", expectedTime, updatedItem.NextAttemptAt)
	}
}

// ---------------------------------------------------------------------------
// stageLoadItemContext tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageLoadItemContext_BasicLoad(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job and item
	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Call stageLoadItemContext
	p := &acquisitionPipeline{}
	skip, err := handler.stageLoadItemContext(p, item.ID)

	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false")
	}
	if p.item.ID != item.ID {
		t.Errorf("expected p.item.ID=%d, got %d", item.ID, p.item.ID)
	}
	if p.item.JobID != job.ID {
		t.Errorf("expected p.item.JobID=%d, got %d", job.ID, p.item.JobID)
	}
	if p.job.ID != job.ID {
		t.Errorf("expected p.job.ID=%d, got %d", job.ID, p.job.ID)
	}
	if p.job.Type != "acquisition" {
		t.Errorf("expected p.job.Type='acquisition', got %s", p.job.Type)
	}
	if p.profile != nil {
		t.Error("expected p.profile to be nil (no quality profile in params)")
	}
}

func TestAcquisitionHandler_StageLoadItemContext_ItemNotFound(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	p := &acquisitionPipeline{}
	_, err := handler.stageLoadItemContext(p, 99999)

	require.Error(t, err, "expected error for non-existent item")
}

func TestAcquisitionHandler_StageLoadItemContext_WithQualityProfile(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create quality profile
	profile := database.QualityProfile{
		Name:           "Test Profile",
		AllowedFormats: "flac,mp3",
		PreferLossless: true,
	}
	require.NoError(t, db.Create(&profile).Error, "failed to create profile")

	// Create job with quality_profile_id in params
	params := struct {
		QualityProfileID string `json:"quality_profile_id"`
	}{QualityProfileID: profile.ID.String()}
	paramsJSON, _ := json.Marshal(params)

	job := database.Job{
		Type:   "acquisition",
		State:  "running",
		Params: paramsJSON,
	}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Call stageLoadItemContext
	p := &acquisitionPipeline{}
	skip, err := handler.stageLoadItemContext(p, item.ID)

	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false")
	}
	require.NotNil(t, p.profile, "expected p.profile to be set")
	if p.profile.ID != profile.ID {
		t.Errorf("expected p.profile.ID=%s, got %s", profile.ID, p.profile.ID)
	}
	if p.profile.Name != "Test Profile" {
		t.Errorf("expected p.profile.Name='Test Profile', got %s", p.profile.Name)
	}
}

func TestAcquisitionHandler_StageLoadItemContext_WithInvalidParamsJSON(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job with invalid JSON in params
	job := database.Job{
		Type:   "acquisition",
		State:  "running",
		Params: []byte("{bad json}"),
	}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Call stageLoadItemContext - should not error even with bad JSON
	p := &acquisitionPipeline{}
	skip, err := handler.stageLoadItemContext(p, item.ID)

	require.NoError(t, err, "unexpected error with bad JSON")
	if skip {
		t.Error("expected skip=false")
	}
	if p.profile != nil {
		t.Error("expected p.profile to be nil with bad JSON params")
	}
}

func TestAcquisitionHandler_StageLoadItemContext_WithNoParams(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job with nil params
	job := database.Job{
		Type:   "acquisition",
		State:  "running",
		Params: nil,
	}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Call stageLoadItemContext
	p := &acquisitionPipeline{}
	skip, err := handler.stageLoadItemContext(p, item.ID)

	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false")
	}
	if p.profile != nil {
		t.Error("expected p.profile to be nil with nil params")
	}
}

// ---------------------------------------------------------------------------
// Execute tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_Execute_EmptyJob(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job with 0 items - with 0 items, loop exits immediately
	// because completed+failed (0) >= total (0)
	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	err := handler.Execute(context.Background(), job.ID, job)
	require.NoError(t, err, "unexpected error")

	// Verify job state is "failed" (since failed (0) == total (0))
	var updatedJob database.Job
	require.NoError(t, db.First(&updatedJob, job.ID).Error, "failed to fetch updated job")
	if updatedJob.State != "failed" {
		t.Errorf("expected state 'failed', got %s", updatedJob.State)
	}

	// Verify summary was updated
	if updatedJob.Summary == "" {
		t.Error("expected summary to be updated")
	}
	if updatedJob.Summary != "Progress: 0/0 (Success: 0, Failed: 0)" {
		t.Errorf("expected summary 'Progress: 0/0 (Success: 0, Failed: 0)', got %s", updatedJob.Summary)
	}
}

func TestAcquisitionHandler_Execute_CancelledContext(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job with 1 item
	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := handler.Execute(ctx, job.ID, job)
	require.Error(t, err, "expected error")
	require.Equal(t, context.Canceled, err, "expected Canceled")
}

func TestAcquisitionHandler_Execute_Timeout(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job with 1 queued item
	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Use a short timeout - the 5s tick never fires, so select picks ctx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := handler.Execute(ctx, job.ID, job)
	require.Error(t, err, "expected error")
	require.Equal(t, context.DeadlineExceeded, err, "expected DeadlineExceeded")
}

// ---------------------------------------------------------------------------
// ExecuteItem test
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_ExecuteItem_ItemNotFound(t *testing.T) {
	db := setupHandlerTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create job (needed for ExecuteItem signature)
	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	// Call ExecuteItem with non-existent itemID - goes through stageLoadItemContext
	err := handler.ExecuteItem(context.Background(), job.ID, 99999)

	require.Error(t, err, "expected error for non-existent item")
}

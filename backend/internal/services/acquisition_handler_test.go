package services

import (
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestAcquisitionHandler_FailItem(t *testing.T) {
	// 1. Setup DB
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	database.Migrate(db)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil)

	// 2. Create job and item
	job := database.Job{Type: "acquisition"}
	db.Create(&job)

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	db.Create(&item)

	// 3. Fail item (1st time)
	handler.failItem(job.ID, item.ID, "test failure")

	// 4. Verify
	var updatedItem database.JobItem
	db.First(&updatedItem, item.ID)

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
	db.First(&updatedItem, item.ID)

	if updatedItem.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %d", updatedItem.RetryCount)
	}

	// Backoff for 1 retry is 5 minutes
	expectedTime := time.Now().Add(5 * time.Minute)
	if updatedItem.NextAttemptAt.Before(expectedTime.Add(-10*time.Second)) || updatedItem.NextAttemptAt.After(expectedTime.Add(10*time.Second)) {
		t.Errorf("expected NextAttemptAt around %v, got %v", expectedTime, updatedItem.NextAttemptAt)
	}
}

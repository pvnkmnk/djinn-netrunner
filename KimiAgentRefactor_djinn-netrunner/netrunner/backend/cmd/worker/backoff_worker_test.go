package main

import (
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestClaimNextJobItem_WithBackoff(t *testing.T) {
	// 1. Setup temporary SQLite DB
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	database.Migrate(db)

	w := &WorkerOrchestrator{db: db}

	// 2. Create a job and items
	job := database.Job{Type: "acquisition", State: "running"}
	db.Create(&job)

	future := time.Now().Add(1 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)

	item1 := database.JobItem{JobID: job.ID, Status: "failed", NextAttemptAt: &future, Sequence: 1, NormalizedQuery: "test1"}
	item2 := database.JobItem{JobID: job.ID, Status: "failed", NextAttemptAt: &past, Sequence: 2, NormalizedQuery: "test2"}
	item3 := database.JobItem{JobID: job.ID, Status: "queued", Sequence: 3, NormalizedQuery: "test3"}

	db.Create(&item1)
	db.Create(&item2)
	db.Create(&item3)

	// 3. Claim item - should skip item1 (future) and pick item2 (past)
	id, err := w.claimNextJobItem(job.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != item2.ID {
		t.Errorf("expected to claim item2 (ID %d), got %d", item2.ID, id)
	}

	// 4. Claim next - should pick item3 (queued)
	id, err = w.claimNextJobItem(job.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != item3.ID {
		t.Errorf("expected to claim item3 (ID %d), got %d", item3.ID, id)
	}

	// 5. Claim next - should return 0
	id, err = w.claimNextJobItem(job.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != 0 {
		t.Errorf("expected 0, got %d", id)
	}
}

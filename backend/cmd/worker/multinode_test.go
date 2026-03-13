package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestMultiNodeJobClaim(t *testing.T) {
	// 1. Setup Shared DB
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	cfg := &config.Config{DatabaseURL: dbPath}
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	database.Migrate(db)

	// 2. Create multiple jobs with DIFFERENT scopes
	numJobs := 10
	for i := 0; i < numJobs; i++ {
		job := database.Job{
			Type:        "acquisition",
			State:       "queued",
			ScopeType:   "artist",
			ScopeID:     fmt.Sprintf("artist-%d", i),
			RequestedAt: time.Now(),
		}
		db.Create(&job)
	}

	// 3. Initialize two workers pointing to the same DB
	w1 := NewWorkerOrchestrator(cfg, db)
	w2 := NewWorkerOrchestrator(cfg, db)

	// Set distinct worker IDs for tracking
	w1.workerID = "worker-1"
	w2.workerID = "worker-2"

	// 4. Concurrent claim test
	var wg sync.WaitGroup
	
	claimFunc := func(w *WorkerOrchestrator) {
		defer wg.Done()
		for i := 0; i < numJobs; i++ {
			// Simulate claim logic
			w.claimAndProcess()
			time.Sleep(10 * time.Millisecond)
		}
	}

	wg.Add(2)
	go claimFunc(w1)
	go claimFunc(w2)
	wg.Wait()

	// 5. Verify no job was claimed by both
	var jobs []database.Job
	db.Find(&jobs)

	claimedCount := 0
	worker1Count := 0
	worker2Count := 0

	for _, j := range jobs {
		if j.State == "running" {
			claimedCount++
			if j.WorkerID != nil {
				if *j.WorkerID == "worker-1" {
					worker1Count++
				} else if *j.WorkerID == "worker-2" {
					worker2Count++
				}
			}
		}
	}

	log.Printf("[TEST] Total jobs: %d, Claimed: %d (W1: %d, W2: %d)", numJobs, claimedCount, worker1Count, worker2Count)

	if claimedCount != numJobs {
		t.Errorf("expected %d claimed jobs, got %d", numJobs, claimedCount)
	}

	// Check for unique claims via logs/counts (if we had more complex tracking)
}

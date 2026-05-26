package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

// BenchmarkJobSelection measures the overhead of the job claim transaction:
// SELECT next queued job + UPDATE to running within an atomic transaction.
func BenchmarkJobSelection(b *testing.B) {
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		b.Fatalf("db connect: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		b.Fatalf("migrate: %v", err)
	}

	for i := 0; i < 100; i++ {
		if err := db.Create(&database.Job{
			Type: "acquisition", State: "queued",
			RequestedAt: time.Now().Add(time.Duration(-i) * time.Minute),
		}).Error; err != nil {
			b.Fatalf("seed job %d: %v", i, err)
		}
	}

	workerID := "bench-worker"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var job database.Job
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("state = ?", "queued").Order("requested_at ASC").First(&job).Error; err != nil {
				return err
			}
			now := time.Now()
			return tx.Model(&job).Where("state = ?", "queued").Updates(map[string]interface{}{
				"state":        "running",
				"worker_id":    workerID,
				"started_at":   &now,
				"heartbeat_at": &now,
			}).Error
		}); err != nil {
			b.Fatalf("claim transaction: %v", err)
		}
		if err := db.Model(&database.Job{}).Where("id = ?", job.ID).Update("state", "queued").Error; err != nil {
			b.Fatalf("reset job: %v", err)
		}
	}
}

// BenchmarkAcquisitionItemProcessing measures item claim throughput:
// finding the next eligible item in a job and marking it as running.
func BenchmarkAcquisitionItemProcessing(b *testing.B) {
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		b.Fatalf("db connect: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		b.Fatalf("migrate: %v", err)
	}

	processor := services.NewJobItemProcessor(db, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	if err := db.Create(&job).Error; err != nil {
		b.Fatalf("create job: %v", err)
	}
	for i := 0; i < 50; i++ {
		if err := db.Create(&database.JobItem{
			JobID: job.ID, Status: "queued", Sequence: i,
			NormalizedQuery: "Artist - Track",
			Artist: "Test Artist", TrackTitle: "Test Track",
		}).Error; err != nil {
			b.Fatalf("seed item %d: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := processor.ClaimNextItem(job.ID); err != nil {
			b.Fatalf("claim item: %v", err)
		}
		// ClaimNextItem sets status to "running"
		if err := db.Model(&database.JobItem{}).Where("job_id = ? AND status = ?", job.ID, "running").
			Update("status", "queued").Error; err != nil {
			b.Fatalf("reset item: %v", err)
		}
	}
}

// BenchmarkMetadataExtraction measures CPU-only metadata utility throughput.
func BenchmarkMetadataExtraction(b *testing.B) {
	ext := services.NewMetadataExtractor()
	libraryRoot := filepath.Join(b.TempDir(), "library")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ext.IsAudioFile("test.flac")
		_ = ext.SanitizeFilename("Test/Artist - Test:Track*Name?.mp3")
		_ = ext.GenerateLibraryPath(&services.AudioMetadata{
			Artist: "Test Artist",
			Title:  "Test Track",
			Album:  "Test Album",
			Format: "flac",
		}, libraryRoot)
	}
}

package main

import (
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
	database.Migrate(db)

	// Seed 100 queued jobs to simulate realistic selection pressure
	for i := 0; i < 100; i++ {
		db.Create(&database.Job{
			Type: "acquisition", State: "queued",
			RequestedAt: time.Now().Add(time.Duration(-i) * time.Minute),
		})
	}

	workerID := "bench-worker"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var job database.Job
		_ = db.Transaction(func(tx *gorm.DB) error {
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
		})
		// Reset for next iteration
		db.Model(&database.Job{}).Where("id = ?", job.ID).Update("state", "queued")
	}
}

// BenchmarkAcquisitionItemProcessing measures item claim throughput:
// finding the next eligible item in a job and marking it as processing.
func BenchmarkAcquisitionItemProcessing(b *testing.B) {
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		b.Fatalf("db connect: %v", err)
	}
	database.Migrate(db)

	processor := services.NewJobItemProcessor(db, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	db.Create(&job)
	for i := 0; i < 50; i++ {
		db.Create(&database.JobItem{
			JobID: job.ID, Status: "queued", Sequence: i,
			NormalizedQuery: "Artist - Track",
			Artist: "Test Artist", TrackTitle: "Test Track",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = processor.ClaimNextItem(job.ID)
		// Reset items for next iteration
		db.Model(&database.JobItem{}).Where("job_id = ? AND status = ?", job.ID, "processing").
			Update("status", "queued")
	}
}

// BenchmarkMetadataExtraction measures CPU-only metadata utility throughput.
func BenchmarkMetadataExtraction(b *testing.B) {
	ext := services.NewMetadataExtractor()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ext.IsAudioFile("test.flac")
		_ = ext.SanitizeFilename("Test/Artist - Test:Track*Name?.mp3")
		_ = ext.GenerateLibraryPath(&services.AudioMetadata{
			Artist: "Test Artist",
			Title:  "Test Track",
			Album:  "Test Album",
			Format: "flac",
		}, "/tmp/library")
	}
}

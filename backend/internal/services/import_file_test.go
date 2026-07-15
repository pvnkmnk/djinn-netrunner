package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMoveFile(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "source.txt")
		dstPath := filepath.Join(dstDir, "dest.txt")

		content := "test content for move"
		if err := os.WriteFile(srcPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write source file: %v", err)
		}

		h := &AcquisitionHandler{}
		cleanupErr, copyErr := h.moveFile(srcPath, dstPath)

		if copyErr != nil {
			t.Fatalf("moveFile returned copy error: %v", copyErr)
		}
		if cleanupErr != nil {
			t.Fatalf("moveFile returned cleanup error: %v", cleanupErr)
		}

		// Verify content moved to dst
		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read dst file: %v", err)
		}
		if string(got) != content {
			t.Errorf("dst content = %q, want %q", string(got), content)
		}

		// Verify src is gone
		if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
			t.Errorf("source file still exists after move")
		}
	})

	t.Run("src not found", func(t *testing.T) {
		h := &AcquisitionHandler{}
		cleanupErr, copyErr := h.moveFile("/nonexistent/file.txt", "/tmp/dest.txt")

		if copyErr == nil {
			t.Fatal("moveFile expected copy error for nonexistent src, got nil")
		}
		if cleanupErr != nil {
			t.Fatalf("moveFile returned unexpected cleanup error: %v", cleanupErr)
		}
	})
}

func TestFailItem(t *testing.T) {
	setupDB := func(t *testing.T) *gorm.DB {
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

	t.Run("first failure gets retry", func(t *testing.T) {
		db := setupDB(t)

		job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
		require.NoError(t, db.Create(&job).Error, "failed to create job")

		item := database.JobItem{JobID: job.ID, Status: "queued", RetryCount: 0}
		require.NoError(t, db.Create(&item).Error, "failed to create item")

		h := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		h.failItem(job.ID, item.ID, "download failed")

		var updated database.JobItem
		require.NoError(t, db.First(&updated, item.ID).Error, "failed to fetch updated item")

		if updated.Status != "failed" {
			t.Errorf("status = %q, want %q", updated.Status, "failed")
		}
		if updated.RetryCount != 1 {
			t.Errorf("retry_count = %d, want %d", updated.RetryCount, 1)
		}
		if updated.FailureReason != "download failed" {
			t.Errorf("failure_reason = %q, want %q", updated.FailureReason, "download failed")
		}
		if updated.NextAttemptAt == nil {
			t.Error("next_attempt_at should be set")
		}
	})

	t.Run("item not found", func(t *testing.T) {
		db := setupDB(t)

		h := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		// Should not panic
		h.failItem(1, 999, "something went wrong")
	})

	t.Run("max attempts exhausted abandons item", func(t *testing.T) {
		db := setupDB(t)

		job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 1}
		require.NoError(t, db.Create(&job).Error, "failed to create job")

		item := database.JobItem{JobID: job.ID, Status: "queued", RetryCount: 0}
		require.NoError(t, db.Create(&item).Error, "failed to create item")

		h := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		h.failItem(job.ID, item.ID, "permanent failure")

		var updated database.JobItem
		require.NoError(t, db.First(&updated, item.ID).Error, "failed to fetch updated item")

		if updated.Status != "abandoned" {
			t.Errorf("status = %q, want %q", updated.Status, "abandoned")
		}
		if updated.RetryCount != 1 {
			t.Errorf("retry_count = %d, want %d", updated.RetryCount, 1)
		}
	})

	t.Run("max attempts zero uses safety default", func(t *testing.T) {
		db := setupDB(t)

		job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 0}
		require.NoError(t, db.Create(&job).Error, "failed to create job")

		item := database.JobItem{JobID: job.ID, Status: "queued", RetryCount: 0}
		require.NoError(t, db.Create(&item).Error, "failed to create item")

		h := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		h.failItem(job.ID, item.ID, "failure with zero max")

		var updated database.JobItem
		require.NoError(t, db.First(&updated, item.ID).Error, "failed to fetch updated item")

		// With safety default of 3, retry_count 0 + 1 = 1 < 3, so not abandoned
		if updated.Status != "failed" {
			t.Errorf("status = %q, want %q", updated.Status, "failed")
		}
	})
}

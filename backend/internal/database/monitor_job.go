package database

import (
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// MonitorJobInterval is how often the worker enqueues a release_monitor job.
// CheckAllArtists skips artists checked within the last 24h, so this cadence
// bounds discography-sync lag without hammering MusicBrainz.
const MonitorJobInterval = 1 * time.Hour

// EnqueueMonitorJobIfIdle creates a system release_monitor job unless one is
// already queued or running. Called once at worker startup (seeding the first
// job) and then hourly by the worker's monitor loop. Because the work runs as
// a real job, it gets claiming, heartbeats, honest finalization, and UI
// visibility; the existence check keeps multiple workers from stacking
// duplicate monitor jobs.
//
// The job has no OwnerUserID (nil): like other system rows it is therefore
// visible to admins and hidden from regular users' scoped views.
func EnqueueMonitorJobIfIdle(db *gorm.DB) bool {
	var count int64
	if err := db.Model(&Job{}).Where("job_type = ? AND state IN ?", "release_monitor", []string{"queued", "running"}).Count(&count).Error; err != nil {
		slog.Error("monitor job: idempotence check failed", "error", err)
		return false
	}
	if count > 0 {
		return false
	}
	job := Job{
		Type:      "release_monitor",
		State:     "queued",
		ScopeType: "system",
		CreatedBy: "scheduler",
	}
	if err := db.Create(&job).Error; err != nil {
		slog.Error("monitor job: enqueue failed", "error", err)
		return false
	}
	slog.Info("monitor job enqueued", "job_id", job.ID)
	return true
}

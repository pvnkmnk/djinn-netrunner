package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
	"gorm.io/gorm"
)

// ZombieRecovery detects jobs whose heartbeats have gone stale ("zombie" jobs)
// and resets them to queued so they can be reclaimed by a healthy worker.
type ZombieRecovery struct {
	db              *gorm.DB
	lockManager     database.LockManager
	staleThreshold  time.Duration
	cleanupInterval time.Duration
}

// ZombieRecoveryConfig holds configurable parameters for ZombieRecovery.
type ZombieRecoveryConfig struct {
	StaleThreshold  time.Duration // how old a heartbeat must be to be considered stale
	CleanupInterval time.Duration // how often to scan for zombies
}

// DefaultZombieRecoveryConfig returns production defaults.
func DefaultZombieRecoveryConfig() ZombieRecoveryConfig {
	return ZombieRecoveryConfig{
		StaleThreshold:  2 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

func NewZombieRecovery(db *gorm.DB, lm database.LockManager, cfg ZombieRecoveryConfig) *ZombieRecovery {
	return &ZombieRecovery{
		db:              db,
		lockManager:     lm,
		staleThreshold:  cfg.StaleThreshold,
		cleanupInterval: cfg.CleanupInterval,
	}
}

// Run starts the zombie cleanup loop, blocking until ctx is cancelled.
func (z *ZombieRecovery) Run(ctx context.Context, workerID string) {
	slog.Info("Starting zombie cleanup loop", "worker_id", workerID)
	ticker := time.NewTicker(z.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			z.cleanup(ctx, workerID)
		}
	}
}

func (z *ZombieRecovery) cleanup(ctx context.Context, workerID string) {
	staleThreshold := time.Now().Add(-z.staleThreshold)
	var zombieJobs []database.Job
	err := z.db.Where("state = ? AND heartbeat_at < ?", "running", staleThreshold).Find(&zombieJobs).Error
	if err != nil {
		slog.Error("Error searching for zombie jobs", "worker_id", workerID, "error", err)
		return
	}

	for _, job := range zombieJobs {
		slog.Warn("Resetting zombie job", "worker_id", workerID, "job_id", job.ID, "last_heartbeat", job.HeartbeatAt)

		z.db.Model(&job).Updates(map[string]interface{}{
			"state":        "queued",
			"worker_id":    nil,
			"started_at":   nil,
			"heartbeat_at": nil,
		})
		metrics.ZombieJobsRecovered.Inc()

		lockKey, err := z.lockManager.GetScopeLockKey(ctx, job.ScopeType, job.ScopeID)
		if err == nil {
			if err := z.lockManager.ReleaseLock(ctx, lockKey); err != nil {
				slog.Warn("Failed to release recovered zombie job lock", "job_id", job.ID, "error", err)
			}
		}
	}
}

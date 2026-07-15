package services

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupZombieTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = database.Migrate(db)
	require.NoError(t, err)
	return db
}

// mockLockManager implements database.LockManager for testing.
type mockLockManager struct {
	database.LockManager
	released     []int64
	scopeKeys    map[string]int64 // map scopeType:scopeID -> key
	keyCounter   int64
}

func newMockLockManager() *mockLockManager {
	return &mockLockManager{
		released:  make([]int64, 0),
		scopeKeys: make(map[string]int64),
		keyCounter: 1,
	}
}

func (m *mockLockManager) ReleaseLock(ctx context.Context, key int64) error {
	m.released = append(m.released, key)
	return nil
}

func (m *mockLockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	key := scopeType + ":" + scopeID
	if k, ok := m.scopeKeys[key]; ok {
		return k, nil
	}
	m.scopeKeys[key] = m.keyCounter
	m.keyCounter++
	return m.keyCounter - 1, nil
}

func (m *mockLockManager) AcquireTryLock(ctx context.Context, key int64) (bool, error) {
	return true, nil
}

func (m *mockLockManager) Close() error {
	return nil
}

func TestDefaultZombieRecoveryConfig(t *testing.T) {
	cfg := DefaultZombieRecoveryConfig()
	assert.Equal(t, 2*time.Minute, cfg.StaleThreshold)
	assert.Equal(t, 1*time.Minute, cfg.CleanupInterval)
}

func TestNewZombieRecovery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	lm := newMockLockManager()
	cfg := DefaultZombieRecoveryConfig()

	zr := NewZombieRecovery(db, lm, cfg)

	assert.Equal(t, db, zr.db)
	assert.Equal(t, lm, zr.lockManager)
	assert.Equal(t, cfg.StaleThreshold, zr.staleThreshold)
	assert.Equal(t, cfg.CleanupInterval, zr.cleanupInterval)
}

func TestZombieRecovery_cleanup_NoZombieJobs(t *testing.T) {
	db := setupZombieTestDB(t)
	lm := newMockLockManager()
	zr := NewZombieRecovery(db, lm, DefaultZombieRecoveryConfig())

	// Run cleanup on empty DB - should not panic or error
	zr.cleanup(context.Background(), "test-worker")

	// Verify no locks were released
	assert.Empty(t, lm.released)

	// Verify no jobs were modified
	var count int64
	db.Model(&database.Job{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestZombieRecovery_cleanup_OneStaleJob(t *testing.T) {
	db := setupZombieTestDB(t)
	lm := newMockLockManager()
	zr := NewZombieRecovery(db, lm, DefaultZombieRecoveryConfig())

	// Create a running job with stale heartbeat
	staleTime := time.Now().Add(-zr.staleThreshold - 1*time.Second)
	job := database.Job{
		Type:        "sync",
		State:       "running",
		WorkerID:    stringPtr("old-worker"),
		ScopeType:   "watchlist",
		ScopeID:     "wl-123",
		HeartbeatAt: &staleTime,
	}
	require.NoError(t, db.Create(&job).Error)

	// Run cleanup
	zr.cleanup(context.Background(), "test-worker")

	// Verify job was reset to queued
	var updatedJob database.Job
	require.NoError(t, db.First(&updatedJob, job.ID).Error)
	assert.Equal(t, "queued", updatedJob.State)
	assert.Nil(t, updatedJob.WorkerID)
	assert.Nil(t, updatedJob.StartedAt)
	assert.Nil(t, updatedJob.HeartbeatAt)

	// Verify lock was released
	assert.Contains(t, lm.released, int64(1)) // scope key for "watchlist:wl-123"
}

func TestZombieRecovery_cleanup_OneFreshJob(t *testing.T) {
	db := setupZombieTestDB(t)
	lm := newMockLockManager()
	zr := NewZombieRecovery(db, lm, DefaultZombieRecoveryConfig())

	// Create a running job with fresh heartbeat
	freshTime := time.Now()
	job := database.Job{
		Type:        "sync",
		State:       "running",
		WorkerID:    stringPtr("healthy-worker"),
		ScopeType:   "watchlist",
		ScopeID:     "wl-456",
		HeartbeatAt: &freshTime,
	}
	require.NoError(t, db.Create(&job).Error)

	// Run cleanup
	zr.cleanup(context.Background(), "test-worker")

	// Verify job was NOT modified
	var updatedJob database.Job
	require.NoError(t, db.First(&updatedJob, job.ID).Error)
	assert.Equal(t, "running", updatedJob.State)
	assert.NotNil(t, updatedJob.WorkerID)
	assert.Equal(t, "healthy-worker", *updatedJob.WorkerID)

	// Verify no locks were released
	assert.Empty(t, lm.released)
}

func TestZombieRecovery_cleanup_MixedStaleAndFresh(t *testing.T) {
	db := setupZombieTestDB(t)
	lm := newMockLockManager()
	zr := NewZombieRecovery(db, lm, DefaultZombieRecoveryConfig())

	staleTime := time.Now().Add(-zr.staleThreshold - 1*time.Second)
	freshTime := time.Now()

	// Stale job
	staleJob := database.Job{
		Type:        "sync",
		State:       "running",
		WorkerID:    stringPtr("stale-worker"),
		ScopeType:   "watchlist",
		ScopeID:     "wl-stale",
		HeartbeatAt: &staleTime,
	}
	require.NoError(t, db.Create(&staleJob).Error)

	// Fresh job
	freshJob := database.Job{
		Type:        "scan",
		State:       "running",
		WorkerID:    stringPtr("healthy-worker"),
		ScopeType:   "library",
		ScopeID:     "lib-123",
		HeartbeatAt: &freshTime,
	}
	require.NoError(t, db.Create(&freshJob).Error)

	// Run cleanup
	zr.cleanup(context.Background(), "test-worker")

	// Verify stale job was reset
	var updatedStaleJob database.Job
	require.NoError(t, db.First(&updatedStaleJob, staleJob.ID).Error)
	assert.Equal(t, "queued", updatedStaleJob.State)
	assert.Nil(t, updatedStaleJob.WorkerID)

	// Verify fresh job was NOT modified
	var updatedFreshJob database.Job
	require.NoError(t, db.First(&updatedFreshJob, freshJob.ID).Error)
	assert.Equal(t, "running", updatedFreshJob.State)
	assert.NotNil(t, updatedFreshJob.WorkerID)
	assert.Equal(t, "healthy-worker", *updatedFreshJob.WorkerID)

	// Verify only one lock was released (for the stale job)
	assert.Len(t, lm.released, 1)
}

func stringPtr(s string) *string {
	return &s
}

package database

import (
	"context"
	"fmt"
	"log"
	"sync"

	"gorm.io/gorm"
)

// LockManager defines the interface for acquiring and releasing locks
type LockManager interface {
	AcquireTryLock(ctx context.Context, key int64) (bool, error)
	ReleaseLock(ctx context.Context, key int64) error
	GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error)
	Close() error
}

// NewLockManager returns the appropriate LockManager based on the database type
func NewLockManager(db *gorm.DB) LockManager {
	dbType := db.Dialector.Name()
	if dbType == "postgres" {
		return &PostgresLockManager{db: db}
	}
	log.Println("[LOCK] Using in-memory lock manager for SQLite")
	return &InMemoryLockManager{
		activeLocks: make(map[int64]bool),
	}
}

// PostgresLockManager handles session-level advisory locks in PostgreSQL
type PostgresLockManager struct {
	db *gorm.DB
}

func (m *PostgresLockManager) AcquireTryLock(ctx context.Context, key int64) (bool, error) {
	var acquired bool
	err := m.db.WithContext(ctx).Raw("SELECT pg_try_advisory_lock(?)", key).Scan(&acquired).Error
	if err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	return acquired, nil
}

func (m *PostgresLockManager) ReleaseLock(ctx context.Context, key int64) error {
	err := m.db.WithContext(ctx).Exec("SELECT pg_advisory_unlock(?)", key).Error
	if err != nil {
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}
	return nil
}

func (m *PostgresLockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	var lockKey int64
	err := m.db.WithContext(ctx).Raw("SELECT scope_lock_key(?, ?)", scopeType, scopeID).Scan(&lockKey).Error
	if err != nil {
		return 0, fmt.Errorf("failed to compute scope lock key: %w", err)
	}
	return lockKey, nil
}

func (m *PostgresLockManager) Close() error {
	return nil
}

// InMemoryLockManager handles locks in memory for standalone SQLite deployments
type InMemoryLockManager struct {
	activeLocks map[int64]bool
	mutex       sync.Mutex
}

func (m *InMemoryLockManager) AcquireTryLock(ctx context.Context, key int64) (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.activeLocks[key] {
		return false, nil
	}

	m.activeLocks[key] = true
	return true, nil
}

func (m *InMemoryLockManager) ReleaseLock(ctx context.Context, key int64) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.activeLocks, key)
	return nil
}

func (m *InMemoryLockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	// Replicate the logic from the SQL function:
	// 1001 * 1000000000 + (hash of scope_type:scopeID)
	// For now, a simple hash using sum of characters or similar
	hash := int64(0)
	combined := fmt.Sprintf("%s:%s", scopeType, scopeID)
	for _, char := range combined {
		hash = 31*hash + int64(char)
	}
	// Keep it within 32-bit for consistency if needed, but int64 is fine
	return 1001*1000000000 + (hash & 0x7FFFFFFF), nil
}

func (m *InMemoryLockManager) Close() error {
	return nil
}

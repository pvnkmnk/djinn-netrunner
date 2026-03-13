package database

import (
	"context"
	"fmt"
	"log"
	"time"

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
	log.Println("[LOCK] Using table-based lock manager for SQLite")
	return &TableLockManager{db: db}
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

// TableLockManager handles distributed locks via a database table
type TableLockManager struct {
	db *gorm.DB
}

func (m *TableLockManager) AcquireTryLock(ctx context.Context, key int64) (bool, error) {
	var acquired bool
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Check for existing active lock
		var count int64
		err := tx.Model(&Lock{}).Where("key = ? AND expires_at > ?", key, time.Now()).Count(&count).Error
		if err != nil {
			return err
		}

		if count > 0 {
			acquired = false
			return nil
		}

		// 2. Upsert lock
		expires := time.Now().Add(15 * time.Minute) // Default timeout
		lock := Lock{
			Key:       key,
			ExpiresAt: expires,
		}
		err = tx.Save(&lock).Error
		if err != nil {
			return err
		}

		acquired = true
		return nil
	})

	if err != nil {
		return false, fmt.Errorf("failed to acquire table lock: %w", err)
	}
	return acquired, nil
}

func (m *TableLockManager) ReleaseLock(ctx context.Context, key int64) error {
	return m.db.WithContext(ctx).Delete(&Lock{}, "key = ?", key).Error
}

func (m *TableLockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	// Replicate the logic from the SQL function:
	// 1001 * 1000000000 + (hash of scope_type:scopeID)
	hash := int64(0)
	combined := fmt.Sprintf("%s:%s", scopeType, scopeID)
	for _, char := range combined {
		hash = 31*hash + int64(char)
	}
	return 1001*1000000000 + (hash & 0x7FFFFFFF), nil
}

func (m *TableLockManager) Close() error {
	return nil
}


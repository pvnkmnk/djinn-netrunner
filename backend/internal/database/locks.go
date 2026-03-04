package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// LockManager manages session-level advisory locks
type LockManager struct {
	db       *gorm.DB
	conn     *sql.Conn
	connMu   sync.Mutex
	locks    map[int64]bool
	locksMu  sync.Mutex
}

// NewLockManager creates a new LockManager
func NewLockManager(db *gorm.DB) *LockManager {
	return &LockManager{
		db:    db,
		locks: make(map[int64]bool),
	}
}

// AcquireTryLock attempts to acquire a session-level advisory lock
func (m *LockManager) AcquireTryLock(ctx context.Context, lockKey int64) (bool, error) {
	m.connMu.Lock()
	defer m.connMu.Unlock()

	if m.conn == nil {
		sqlDB, err := m.db.DB()
		if err != nil {
			return false, fmt.Errorf("failed to get sql.DB: %w", err)
		}
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get dedicated connection: %w", err)
		}
		m.conn = conn
	}

	var acquired bool
	err := m.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("failed to try advisory lock: %w", err)
	}

	if acquired {
		m.locksMu.Lock()
		m.locks[lockKey] = true
		m.locksMu.Unlock()
	}

	return acquired, nil
}

// ReleaseLock releases a session-level advisory lock
func (m *LockManager) ReleaseLock(ctx context.Context, lockKey int64) error {
	m.connMu.Lock()
	defer m.connMu.Unlock()

	if m.conn == nil {
		return nil // No connection, no locks held
	}

	var released bool
	err := m.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey).Scan(&released)
	if err != nil {
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}

	m.locksMu.Lock()
	delete(m.locks, lockKey)
	m.locksMu.Unlock()

	return nil
}

// Close releases all locks and closes the dedicated connection
func (m *LockManager) Close() error {
	m.connMu.Lock()
	defer m.connMu.Unlock()

	if m.conn == nil {
		return nil
	}

	// Session-level locks are automatically released when the connection is closed
	err := m.conn.Close()
	m.conn = nil
	return err
}

// GetScopeLockKey computes the advisory lock key for a given scope
func (m *LockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	var lockKey int64
	err := m.db.Raw("SELECT scope_lock_key(?, ?)", scopeType, scopeID).Scan(&lockKey).Error
	if err != nil {
		return 0, fmt.Errorf("failed to compute scope lock key: %w", err)
	}
	return lockKey, nil
}

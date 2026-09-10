package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
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
	slog.Info("Using table-based lock manager for SQLite")
	return &TableLockManager{db: db}
}

// PostgresLockManager implements distributed locking with PostgreSQL
// session-level advisory locks.
//
// A session-level advisory lock belongs to the database *session*
// (connection), not the transaction. Running pg_try_advisory_lock through
// GORM's connection pool acquires it on whichever pooled connection the
// query lands on, and a later pg_advisory_unlock may run on a *different*
// connection — where it silently no-ops. The lock then leaks until that
// idle backend is terminated, starving every job for the same scope
// (observed live as "Scope locked, requeueing" forever).
//
// To keep the semantics correct, AcquireTryLock reserves a dedicated
// connection from the underlying *sql.DB, acquires the advisory lock on
// that exact session, and pins the connection in the manager. ReleaseLock
// unlocks and returns the connection to the pool on the same session the
// lock lives on. If this process dies, PostgreSQL releases session-level
// locks automatically when the sessions end — a crash can no longer poison
// a scope indefinitely.
type PostgresLockManager struct {
	db *gorm.DB

	mu    sync.Mutex
	locks map[int64]*sql.Conn
}

// held returns the tracked connection for a key (caller must hold m.mu).
func (m *PostgresLockManager) held(key int64) (*sql.Conn, bool) {
	if m.locks == nil {
		return nil, false
	}
	conn, ok := m.locks[key]
	return conn, ok
}

// track records a lock's connection, initializing the map lazily so the
// struct is safe to use regardless of how it was constructed
// (NewLockManager and NewPostgresLockManager both end up here).
func (m *PostgresLockManager) track(key int64, conn *sql.Conn) {
	m.mu.Lock()
	if m.locks == nil {
		m.locks = make(map[int64]*sql.Conn)
	}
	m.locks[key] = conn
	m.mu.Unlock()
}

func NewPostgresLockManager(db *gorm.DB) *PostgresLockManager {
	return &PostgresLockManager{db: db, locks: make(map[int64]*sql.Conn)}
}

// sqlDB extracts the underlying *sql.DB from a GORM handle.
func sqlDB(db *gorm.DB) (*sql.DB, error) {
	if sqlDB, ok := db.ConnPool.(*sql.DB); ok {
		return sqlDB, nil
	}
	return nil, fmt.Errorf("connection pool is not *sql.DB (got %T)", db.ConnPool)
}

func (m *PostgresLockManager) AcquireTryLock(ctx context.Context, key int64) (bool, error) {
	pool, err := sqlDB(m.db)
	if err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	// Reserve a dedicated connection so the advisory lock has a stable session.
	conn, err := pool.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		_ = conn.Close()
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	if !acquired {
		// Lock is held elsewhere; return the reserved connection immediately.
		_ = conn.Close()
		return false, nil
	}

	m.track(key, conn)
	return true, nil
}

func (m *PostgresLockManager) ReleaseLock(ctx context.Context, key int64) error {
	m.mu.Lock()
	conn, ok := m.held(key)
	if ok {
		delete(m.locks, key)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("no advisory lock held for key %d", key)
	}

	// Unlock and return the connection on the same session the lock lives on.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", key); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("failed to release advisory lock connection: %w", err)
	}
	return nil
}

func (m *PostgresLockManager) GetScopeLockKey(ctx context.Context, scopeType, scopeID string) (int64, error) {
	// Compute lock key: 1001 * 1000000000 + (hash of scope_type:scopeID)
	// This matches the logic in TableLockManager
	hash := int64(0)
	combined := fmt.Sprintf("%s:%s", scopeType, scopeID)
	for _, char := range combined {
		hash = 31*hash + int64(char)
	}
	return 1001*1000000000 + (hash & 0x7FFFFFFF), nil
}

// Close releases every advisory lock still held by this manager, best-effort.
// It is registered as a process-exit safety net in main; PostgreSQL would also
// drop the locks when the sessions close, but explicit release keeps the
// backends reusable right away.
func (m *PostgresLockManager) Close() error {
	m.mu.Lock()
	keys := make([]int64, 0, len(m.locks))
	for key := range m.locks {
		keys = append(keys, key)
	}
	m.mu.Unlock()

	for _, key := range keys {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := m.ReleaseLock(ctx, key); err != nil {
			slog.Warn("Failed to release advisory lock during close", "key", key, "error", err)
		}
		cancel()
	}
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

package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"gorm.io/gorm"
)

// setupPostgresForLocks connects using DATABASE_URL, skipping when absent
// (unit CI has no Postgres) or unreachable. Returns the manager plus the gorm
// handle for raw assertions.
func setupPostgresForLocks(t *testing.T) (*PostgresLockManager, *gorm.DB) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(&config.Config{DatabaseURL: dbURL})
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}
	if db.Dialector.Name() != "postgres" {
		t.Skip("DATABASE_URL is not postgres")
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return NewPostgresLockManager(db), db
}

// pgAdvisoryLockCount counts live session-level advisory locks for a key as
// reported by PostgreSQL itself — not what the manager believes.
func pgAdvisoryLockCount(t *testing.T, db *gorm.DB, key int64) int64 {
	t.Helper()
	var count int64
	db.Raw(`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND objid = ?`, key).Scan(&count)
	return count
}

// TestPostgresLockManager_ReleaseDropsSessionLock is THE regression test for
// the leaked-lock incident: the old implementation acquired through the GORM
// pool and unlocked on an arbitrary pooled connection, so pg_advisory_unlock
// silently no-oped and the lock lingered in pg_locks forever, starving every
// job for the scope ("Scope locked, requeueing" loop).
//
// It asserts against the server's own view (pg_locks), not the manager's.
func TestPostgresLockManager_ReleaseDropsSessionLock(t *testing.T) {
	lm, db := setupPostgresForLocks(t)
	ctx := context.Background()
	key := int64(987654321)

	if n := pgAdvisoryLockCount(t, db, key); n != 0 {
		t.Fatalf("precondition: key %d already locked (%d locks)", key, n)
	}

	acquired, err := lm.AcquireTryLock(ctx, key)
	if err != nil {
		t.Fatalf("AcquireTryLock: %v", err)
	}
	if !acquired {
		t.Fatal("expected lock to be acquired")
	}
	if n := pgAdvisoryLockCount(t, db, key); n != 1 {
		t.Fatalf("expected exactly 1 advisory lock after acquire, got %d", n)
	}

	if err := lm.ReleaseLock(ctx, key); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	if n := pgAdvisoryLockCount(t, db, key); n != 0 {
		t.Fatalf("LEAKED LOCK: %d advisory lock(s) for key %d still held in pg_locks after ReleaseLock", n, key)
	}
}

// TestPostgresLockManager_ExclusiveAcrossSessions proves the lock actually
// excludes other sessions — the point of a distributed lock. The old pooled
// implementation could hand the "same" lock to whoever landed on the lucky
// connection that happened to hold it, while blocking everyone else forever.
func TestPostgresLockManager_ExclusiveAcrossSessions(t *testing.T) {
	lm, db := setupPostgresForLocks(t)
	ctx := context.Background()
	key := int64(987654322)

	acquired, err := lm.AcquireTryLock(ctx, key)
	if err != nil || !acquired {
		t.Fatalf("expected initial acquire, got acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = lm.ReleaseLock(context.Background(), key) }()

	// A second, independent session must see the lock as taken.
	pool, err := sqlDB(db)
	if err != nil {
		t.Fatalf("underlying pool: %v", err)
	}
	other, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve second session: %v", err)
	}
	defer func() { _ = other.Close() }()

	var second bool
	if err := other.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&second); err != nil {
		t.Fatalf("second-session try-lock: %v", err)
	}
	if second {
		t.Fatal("second session acquired the same lock — mutual exclusion broken")
	}

	// After release, the second session must be able to take it.
	if err := lm.ReleaseLock(ctx, key); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	var third bool
	if err := other.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&third); err != nil {
		t.Fatalf("post-release try-lock: %v", err)
	}
	if !third {
		t.Fatal("lock still exclusive after ReleaseLock — unlock did not reach the server")
	}
	_, _ = other.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", key)
}

// TestPostgresLockManager_ReleaseUnknownKeyIsError documents that releasing a
// key the manager never acquired is an error (the manager tracks what it
// holds; callers should not spray unlocks).
func TestPostgresLockManager_ReleaseUnknownKeyIsError(t *testing.T) {
	lm, _ := setupPostgresForLocks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lm.ReleaseLock(ctx, 424242); err == nil {
		t.Fatal("expected error when releasing a never-acquired key")
	}
}

// TestPostgresLockManager_ClosedConnFreesLock verifies the crash story: if the
// process dies (connections close without unlock), PostgreSQL drops the locks.
// We simulate by reserving a raw conn, locking on it, then force-closing it.
func TestPostgresLockManager_ClosedConnFreesLock(t *testing.T) {
	_, db := setupPostgresForLocks(t)
	ctx := context.Background()
	key := int64(987654323)

	pool, err := sqlDB(db)
	if err != nil {
		t.Fatalf("underlying pool: %v", err)
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve conn: %v", err)
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil || !acquired {
		t.Fatalf("lock on raw conn: acquired=%v err=%v", acquired, err)
	}

	// Simulate process death: destroy the session without unlocking.
	if err := conn.Raw(func(driverConn any) error {
		return driverConn.(interface{ Close() error }).Close()
	}); err != nil {
		t.Fatalf("force close: %v", err)
	}
	_ = conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if pgAdvisoryLockCount(t, db, key) == 0 {
			return // server freed the session lock — the crash safety property
		}
		if time.Now().After(deadline) {
			t.Fatal("session-level lock survived session death — would poison the scope after a crash")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

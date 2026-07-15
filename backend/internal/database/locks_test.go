package database

import (
	"context"
	"os"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestLockManager(t *testing.T) {
	// Skip if no database URL
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		// Skip on connection failure (e.g., DNS resolution error on Windows)
		t.Skipf("Failed to connect to database: %v", err)
	}

	lm := NewLockManager(db)
	defer lm.Close()

	ctx := context.Background()
	lockKey := int64(123456789)

	// Acquire lock
	acquired, err := lm.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("Expected lock to be acquired")
	}

	// Try to acquire again (same session should succeed in Postgres, but our manager tracks it)
	// Actually Postgres allowing re-acquisition in same session is fine.
	
	// Release lock
	err = lm.ReleaseLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}
}

func TestGetScopeLockKey(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		// Skip on connection failure (e.g., DNS resolution error on Windows)
		t.Skipf("Failed to connect to database: %v", err)
	}

	lm := NewLockManager(db)
	defer lm.Close()

	ctx := context.Background()
	key, err := lm.GetScopeLockKey(ctx, "artist", "test-artist-id")
	if err != nil {
		t.Fatalf("Failed to get scope lock key: %v", err)
	}

	if key == 0 {
		t.Fatal("Expected non-zero key")
	}
}

func TestPostgresLockManager_AcquireTryLock(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	// Only run for Postgres
	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to configured Postgres database: %v", err)
	}

	lm := &PostgresLockManager{db: db}
	ctx := context.Background()
	lockKey := int64(987654321)

	// Acquire lock
	acquired, err := lm.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("Expected lock to be acquired")
	}

	// Clean up
	_ = lm.ReleaseLock(ctx, lockKey)
}

func TestPostgresLockManager_ReleaseLock(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to configured Postgres database: %v", err)
	}

	lm := &PostgresLockManager{db: db}
	ctx := context.Background()
	lockKey := int64(123123123)

	// Acquire first
	_, err = lm.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Release
	err = lm.ReleaseLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	// Should be able to re-acquire after release
	acquired, err := lm.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to re-acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("Expected lock to be re-acquired after release")
	}

	// Clean up
	_ = lm.ReleaseLock(ctx, lockKey)
}

func TestPostgresLockManager_AcquireTryLock_Duplicate(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to configured Postgres database: %v", err)
	}

	lm := &PostgresLockManager{db: db}
	ctx := context.Background()
	lockKey := int64(555666777)

	// First acquisition should succeed
	acquired1, err := lm.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to acquire lock first time: %v", err)
	}
	if !acquired1 {
		t.Fatal("Expected first lock acquisition to succeed")
	}

	// PG advisory locks are re-entrant within the same session.
	// To test mutual exclusion, use two separate DB connections (sessions).
	cfg2 := &config.Config{DatabaseURL: dbURL}
	db2, err := Connect(cfg2)
	if err != nil {
		t.Fatalf("Failed to open second connection: %v", err)
	}
	defer func() { sqlDB2, _ := db2.DB(); if sqlDB2 != nil { sqlDB2.Close() } }()

	lm2 := &PostgresLockManager{db: db2}

	// Second acquisition from a DIFFERENT session should fail
	acquired2, err := lm2.AcquireTryLock(ctx, lockKey)
	if err != nil {
		t.Fatalf("Failed to attempt second lock: %v", err)
	}
	if acquired2 {
		t.Fatal("Expected second lock acquisition to fail from different session")
	}

	// Clean up
	_ = lm.ReleaseLock(ctx, lockKey)
}

func TestNewLockManager_Postgres(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to configured Postgres database: %v", err)
	}

	lm := NewLockManager(db)
	defer lm.Close()

	// Verify it returns PostgresLockManager type
	_, ok := lm.(*PostgresLockManager)
	if !ok {
		t.Fatal("Expected *PostgresLockManager for Postgres database")
	}
}

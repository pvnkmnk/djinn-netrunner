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
		t.Fatalf("Failed to connect: %v", err)
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
		t.Fatalf("Failed to connect: %v", err)
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

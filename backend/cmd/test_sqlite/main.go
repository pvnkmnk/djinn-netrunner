package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func main() {
	// 1. Setup SQLite config
	dbFile := "test_standalone.db"
	defer os.Remove(dbFile)

	cfg := &config.Config{
		DatabaseURL: dbFile,
	}

	// 2. Connect
	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("Failed to connect to SQLite", "error", err)
		os.Exit(1)
	}

	// 3. Migrate
	err = database.Migrate(db)
	if err != nil {
		slog.Error("Failed to migrate SQLite", "error", err)
		os.Exit(1)
	}

	slog.Info("Successfully connected and migrated SQLite database!")

	// 4. Test LockManager
	lm := database.NewLockManager(db)
	ctx := context.Background()

	key, _ := lm.GetScopeLockKey(ctx, "artist", "test-123")
	acquired, err := lm.AcquireTryLock(ctx, key)
	if err != nil || !acquired {
		slog.Error("Failed to acquire lock", "error", err)
		os.Exit(1)
	}
	slog.Info("Successfully acquired lock", "key", key)

	err = lm.ReleaseLock(ctx, key)
	if err != nil {
		slog.Error("Failed to release lock", "error", err)
		os.Exit(1)
	}
	slog.Info("Successfully released lock!")
}

package main

import (
	"context"
	"log"
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
		log.Fatalf("Failed to connect to SQLite: %v", err)
	}

	// 3. Migrate
	err = database.Migrate(db)
	if err != nil {
		log.Fatalf("Failed to migrate SQLite: %v", err)
	}

	log.Println("Successfully connected and migrated SQLite database!")

	// 4. Test LockManager
	lm := database.NewLockManager(db)
	ctx := context.Background()

	key, _ := lm.GetScopeLockKey(ctx, "artist", "test-123")
	acquired, err := lm.AcquireTryLock(ctx, key)
	if err != nil || !acquired {
		log.Fatalf("Failed to acquire lock: %v", err)
	}
	log.Printf("Successfully acquired lock: %d", key)

	err = lm.ReleaseLock(ctx, key)
	if err != nil {
		log.Fatalf("Failed to release lock: %v", err)
	}
	log.Println("Successfully released lock!")
}

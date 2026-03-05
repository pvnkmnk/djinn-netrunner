package main

import (
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestListenNotifyInterop(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	cfg, _ := config.Load()
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	worker := NewWorkerOrchestrator(cfg, db)
	go worker.listenForWakeup()

	// Wait for listener to start
	time.Sleep(2 * time.Second)

	// Simulate Python side by sending NOTIFY
	err = db.Exec("NOTIFY opswakeup").Error
	if err != nil {
		t.Fatalf("Failed to send NOTIFY: %v", err)
	}

	// Verify worker received it
	select {
	case <-worker.wakeupChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("Timed out waiting for wakeup notification")
	}
}

package main

import (
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// TestListenNotifyInterop proves the wakeup path against a real Postgres: the
// orchestrator's listener is up, a NOTIFY on `opswakeup` reaches it, and the
// worker's channel is signalled.
//
// The guard here used to accept only a DSN whose first ten characters were
// "postgresql", so a `postgres://` URL -- the form database.IsPostgres accepts,
// and the form compose and the integration stack set -- fell through to
// t.Skip("This test only runs on PostgreSQL"). The test had therefore never run
// on the driver production uses, which is the "green because it skipped" hole
// scripts/postgres_gate.py now gates on; the predicate is the app's own.
func TestListenNotifyInterop(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Only run on PostgreSQL - LISTEN/NOTIFY is PostgreSQL-specific
	if !database.IsPostgres(dbURL) {
		t.Skip("This test only runs on PostgreSQL")
	}

	// The URL this test was pointed at is the one under test, so it is passed
	// through explicitly rather than re-read from config: config.Load() can
	// refuse a production-shaped environment, and a skip from there would
	// report as green.
	db, err := database.Connect(&config.Config{DatabaseURL: dbURL})
	if err != nil {
		t.Fatalf("connect to the database under test: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	worker := NewWorkerOrchestrator(&config.Config{DatabaseURL: dbURL}, db)

	// Start listening in background
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("listenForWakeup panicked: %v", r)
			}
		}()
		worker.listenForWakeup()
		// Blocks until the orchestrator's context is cancelled, which nothing
		// in this test does -- the process ends with it.
	}()

	// Wait for the listener to be up before NOTIFYing: pq.NewListener connects
	// asynchronously, and a NOTIFY sent inside that window is simply lost.
	time.Sleep(500 * time.Millisecond)

	// Send NOTIFY using a raw connection to ensure it works
	err = db.Exec("NOTIFY opswakeup").Error
	if err != nil {
		t.Fatalf("Failed to send NOTIFY: %v", err)
	}

	// Verify worker received it - give it a bit more time
	select {
	case <-worker.wakeupChan:
		// Success - notification received
	case <-time.After(5 * time.Second):
		// Failing, not logging. The old version recorded "this may be a test
		// environment limitation" and passed, so a broker that never notified
		// was indistinguishable from a healthy one -- and the worker's
		// five-second poll hides the same regression in production.
		t.Fatal("no wakeup within 5s of NOTIFY opswakeup: the LISTEN/NOTIFY path is not notifying (the worker's poll would mask this)")
	}
}

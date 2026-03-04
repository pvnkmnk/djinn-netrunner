package database

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestConnect(t *testing.T) {
	// This test requires a running PostgreSQL instance.
	// For now, we'll just check if it fails gracefully with an invalid URL.
	cfg := &config.Config{
		DatabaseURL: "postgres://nonexistent:5432/netrunner",
	}

	db, err := Connect(cfg)
	if err == nil {
		t.Error("Expected error with invalid database URL, but got nil")
	}
	if db != nil {
		t.Error("Expected nil DB with invalid database URL")
	}
}

package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set required environment variable
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("MUSICBRAINZ_API_KEY", "test-key")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MUSICBRAINZ_API_KEY")

	// Pass a non-existent file to avoid loading real .env
	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/db" {
		t.Errorf("Expected DatabaseURL to be 'postgres://user:pass@localhost:5432/db', got '%s'", cfg.DatabaseURL)
	}

	if cfg.Environment != "development" {
		t.Errorf("Expected default Environment to be 'development', got '%s'", cfg.Environment)
	}

	if cfg.MusicBrainzAPIKey != "test-key" {
		t.Errorf("Expected MusicBrainzAPIKey to be 'test-key', got '%s'", cfg.MusicBrainzAPIKey)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	// Ensure DATABASE_URL is NOT set
	os.Unsetenv("DATABASE_URL")

	_, err := Load(".non-existent-env")
	if err == nil {
		t.Fatal("Expected error when DATABASE_URL is missing, but got nil")
	}
}

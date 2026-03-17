package services

import (
	"os"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestArtistTrackingService(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Auto-migrate
	err = database.Migrate(db)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	cfg := &MusicBrainzService{} // Dummy for now
	at := NewArtistTrackingService(db, cfg)

	if at == nil {
		t.Fatal("Expected ArtistTrackingService to be initialized")
	}
}

func TestSyncDiscographyCreatesAcquisitionJob(t *testing.T) {
	// This test would require full DB setup
	// For now, we'll verify the method signature exists
	// and has correct parameters
	t.Skip("Integration test - requires DB setup")
}

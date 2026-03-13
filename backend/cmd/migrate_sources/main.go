package main

import (
	"log"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// 0. Run AutoMigrate to ensure tables exist
	if err := database.Migrate(db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// 1. Ensure a default Quality Profile exists
	var profile database.QualityProfile
	err = db.Where("is_default = ?", true).First(&profile).Error
	if err != nil {
		log.Println("Creating default quality profile...")
		profile = database.QualityProfile{
			ID:             uuid.New(),
			Name:           "Default (Lossless)",
			PreferLossless: true,
			MinBitrate:     320,
			IsDefault:      true,
		}
		if err := db.Create(&profile).Error; err != nil {
			log.Fatalf("failed to create default profile: %v", err)
		}
	}

	// 2. Fetch all Sources
	var sources []database.Source
	if err := db.Find(&sources).Error; err != nil {
		log.Fatalf("failed to fetch sources: %v", err)
	}

	log.Printf("Migrating %d sources to watchlists...", len(sources))

	for _, s := range sources {
		log.Printf("Migrating: %s (%s)", s.DisplayName, s.SourceURI)

		// Check if already exists in Watchlist
		var existing database.Watchlist
		err := db.Where("source_uri = ?", s.SourceURI).First(&existing).Error
		if err == nil {
			log.Printf("Watchlist already exists for %s, skipping creation.", s.SourceURI)
			continue
		}

		watchlist := database.Watchlist{
			ID:               uuid.New(),
			Name:             s.DisplayName,
			SourceType:       s.SourceType,
			SourceURI:        s.SourceURI,
			QualityProfileID: profile.ID,
			LastSyncedAt:     s.LastSyncedAt,
			Enabled:          s.SyncEnabled,
			OwnerUserID:      s.OwnerUserID,
			CreatedAt:        s.CreatedAt,
			UpdatedAt:        s.UpdatedAt,
		}

		if err := db.Create(&watchlist).Error; err != nil {
			log.Printf("failed to migrate source %d: %v", s.ID, err)
			continue
		}

		// Note: We are keeping the Source record for now to avoid breaking SyncHandler 
		// until it is refactored in Task 1.2.
	}

	log.Println("Migration complete!")
}

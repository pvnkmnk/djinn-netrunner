package database

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate runs all database migrations
func Migrate(db *gorm.DB) error {
	// Enable UUID extension for Postgres
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\"").Error; err != nil {
			return fmt.Errorf("failed to enable uuid-ossp extension: %w", err)
		}
	}

	// Auto-migrate all models
	if err := db.AutoMigrate(
		&User{},
		&Session{},
		&QualityProfile{},
		&MonitoredArtist{},
		&TrackedRelease{},
		&Source{},
		&Job{},
		&JobLog{},
		&JobItem{},
		&Acquisition{},
		&Library{},
		&Track{},
		&Schedule{},
		&MetadataCache{},
	); err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}

	return nil
}

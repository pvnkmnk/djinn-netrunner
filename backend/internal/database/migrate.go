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

		// Convert legacy ENUM columns to text so GORM AutoMigrate can manage them.
		// The init schema (01-schema.sql) uses PostgreSQL ENUM types for job state
		// columns, but the GORM models use string fields. GORM cannot ALTER ENUM
		// to text without explicit casting.
		//
		// The init schema also uses column name "jobtype" but GORM expects "job_type".
		// If both exist (GORM created job_type text column during partial migration
		// before failing), the duplicate column is dropped so the rename succeeds.
		//
		// All steps are idempotent — they safely handle fresh DBs, partially migrated
		// DBs, and fully migrated DBs.

		// Step 1: Drop GORM's duplicate job_type column if it exists alongside jobtype.
		db.Exec("ALTER TABLE jobs DROP COLUMN IF EXISTS job_type")

		// Step 2: Rename jobtype → job_type and convert to text.
		// Only runs if jobtype column exists and is the ENUM type.
		var jobtypeType string
		db.Raw(`
			SELECT format_type(atttypid, atttypmod)
			FROM pg_attribute
			JOIN pg_class ON attrelid = pg_class.oid
			JOIN pg_namespace ON relnamespace = pg_namespace.oid
			WHERE attname = 'jobtype' AND relname = 'jobs' AND nspname = 'public'`).
			Scan(&jobtypeType)
		if jobtypeType == "jobtype" {
			if err := db.Exec("ALTER TABLE jobs ALTER COLUMN jobtype TYPE text USING jobtype::text").Error; err != nil {
				return fmt.Errorf("failed to convert jobs.jobtype enum to text: %w", err)
			}
			if err := db.Exec("ALTER TABLE jobs RENAME COLUMN jobtype TO job_type").Error; err != nil {
				return fmt.Errorf("failed to rename jobs.jobtype to job_type: %w", err)
			}
		}

		// Step 3: Convert remaining ENUM columns to text (idempotent if already text or gone).
		for _, m := range []struct{ table, column, enumType string }{
			{"jobs", "state", "jobstate"},
			{"jobitems", "status", "jobitemstatus"},
		} {
			var exists bool
			db.Raw("SELECT EXISTS(SELECT 1 FROM pg_type WHERE typname = $1)", m.enumType).Scan(&exists)
			if !exists {
				continue
			}
			var colType string
			db.Raw(`
				SELECT format_type(atttypid, atttypmod)
				FROM pg_attribute
				JOIN pg_class ON attrelid = pg_class.oid
				JOIN pg_namespace ON relnamespace = pg_namespace.oid
				WHERE attname = $1 AND relname = $2 AND nspname = 'public'`,
				m.column, m.table).Scan(&colType)
			if colType == m.enumType {
				sql := fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE text USING %s::text`, m.table, m.column, m.column)
				if err := db.Exec(sql).Error; err != nil {
					return fmt.Errorf("failed to convert %s.%s enum to text: %w", m.table, m.column, err)
				}
			}
			// Drop the unused ENUM type (CASCADE drops dependent defaults).
			db.Exec(fmt.Sprintf("DROP TYPE IF EXISTS %s", m.enumType))
		}
	}

	// Auto-migrate all models
	if err := db.AutoMigrate(
		&User{},
		&Session{},
		&QualityProfile{},
		&MonitoredArtist{},
		&TrackedRelease{},
		&Watchlist{},
		&SpotifyToken{},
		&Job{}, &JobLog{},
		&JobItem{},
		&Acquisition{}, &Library{},
		&Track{},
		&Schedule{},
		&MetadataCache{},
		&Lock{},
		&Setting{},
	); err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}

	return nil
}

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

		// Drop the global unique index on music_brainz_id that was replaced by
		// a composite (owner_user_id, musicbrainz_id) index — AutoMigrate won't drop it.
		if db.Migrator().HasTable("monitored_artists") {
			db.Exec("DROP INDEX IF EXISTS idx_monitored_artists_musicbrainz_id")
		}

		// Remove the DEFAULT true on prefer_web_releases that was removed from the model.
		// AutoMigrate is additive and won't drop column defaults.
		if db.Migrator().HasTable("quality_profiles") && db.Migrator().HasColumn("quality_profiles", "prefer_web_releases") {
			db.Exec("ALTER TABLE quality_profiles ALTER COLUMN prefer_web_releases DROP DEFAULT")
		}

		// Convert legacy ENUM columns to text so GORM AutoMigrate can manage them.
		// These fixup steps only run if the tables already exist (legacy migration scenario).
		if db.Migrator().HasTable("jobs") {
			// Step 1: Handle legacy jobtype/job_type safely.
			hasJobType := db.Migrator().HasColumn("jobs", "job_type")
			hasLegacyJobtype := db.Migrator().HasColumn("jobs", "jobtype")

			if hasJobType && hasLegacyJobtype {
				db.Exec("ALTER TABLE jobs ALTER COLUMN jobtype TYPE text USING jobtype::text")
				db.Exec("UPDATE jobs SET job_type = COALESCE(NULLIF(job_type, ''), jobtype) WHERE job_type IS NULL OR job_type = ''")
				db.Exec("ALTER TABLE jobs DROP COLUMN IF EXISTS jobtype")
			}

			// Step 2: Rename jobtype → job_type and convert to text.
			if !hasJobType && hasLegacyJobtype {
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
				}
				if err := db.Exec("ALTER TABLE jobs RENAME COLUMN jobtype TO job_type").Error; err != nil {
					return fmt.Errorf("failed to rename jobs.jobtype to job_type: %w", err)
				}
				hasJobType = true
			}

			// Step 2b: Ensure job_type is backfilled before AutoMigrate can enforce NOT NULL.
			if hasJobType {
				db.Exec("UPDATE jobs SET job_type = COALESCE(NULLIF(job_type, ''), 'sync') WHERE job_type IS NULL OR job_type = ''")
			}
		}


		// Step 3: Convert remaining ENUM columns to text (idempotent if already text or gone).
		for _, m := range []struct{ table, column, enumType string }{
			{"jobs", "state", "jobstate"},
			{"jobitems", "status", "jobitemstatus"},
		} {
			if !db.Migrator().HasTable(m.table) {
				continue
			}

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
				// Identifiers from hardcoded struct literal — use quoted identifiers for safety
				if err := db.Exec(
					`ALTER TABLE "` + m.table + `" ALTER COLUMN "` + m.column + `" TYPE text USING "` + m.column + `"::text`,
				).Error; err != nil {
					return fmt.Errorf("failed to convert %s.%s enum to text: %w", m.table, m.column, err)
				}
			}
			// Drop the unused ENUM type (CASCADE drops dependent defaults).
			if err := db.Exec(`DROP TYPE IF EXISTS "` + m.enumType + `"`).Error; err != nil {
				return fmt.Errorf("failed to drop enum %s: %w", m.enumType, err)
			}
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
		&Playlist{},
		&PlaylistTrack{},
		&Schedule{},
		&MetadataCache{},
		&Lock{},
		&Setting{},
		&AuditLog{},
		&PeerReputation{},
	); err != nil {
		return fmt.Errorf("failed to auto-migrate: %w", err)
	}

	return nil
}

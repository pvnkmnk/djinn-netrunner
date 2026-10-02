package database

import (
	"fmt"
	"time"

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
		db.Exec("DROP INDEX IF EXISTS idx_monitored_artists_musicbrainz_id")

		// Remove the DEFAULT true on prefer_web_releases that was removed from the model.
		// AutoMigrate is additive and won't drop column defaults.
		if db.Migrator().HasTable("quality_profiles") && db.Migrator().HasColumn("quality_profiles", "prefer_web_releases") {
			db.Exec("ALTER TABLE quality_profiles ALTER COLUMN prefer_web_releases DROP DEFAULT")
		}

		// Remove the DEFAULT 0 on acoustid_score. The model made the column
		// nullable because "never scored" and "scored zero" are different facts,
		// and a default of 0 makes every writer that omits the column claim a
		// measurement nobody took - which is how all 144 legacy rows came to
		// report a score while no lookup had ever run. AutoMigrate won't drop it,
		// so a deployed database would otherwise keep inventing zeros forever.
		if db.Migrator().HasTable("acquisitions") && db.Migrator().HasColumn("acquisitions", "acoustid_score") {
			// Checked, not discarded: if the ALTER fails the default survives and
			// every later insert that omits the column records a zero nobody
			// measured - the exact confusion this slice exists to remove, and one
			// the backfill below can never repair because it runs only once.
			if err := db.Exec("ALTER TABLE acquisitions ALTER COLUMN acoustid_score DROP DEFAULT").Error; err != nil {
				return fmt.Errorf("failed to drop the acoustid_score default: %w", err)
			}
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

	// Backfill the artist release counters.
	//
	// total_releases and acquired_releases were declared on MonitoredArtist but
	// never written by anything, so every row has sat at 0 while
	// tracked_releases holds the real counts - which is why every artist read
	// "Releases: 0/0" no matter how much had been acquired. Only rows that
	// disagree are touched, so this is a no-op once
	// services.RefreshArtistReleaseCounters is maintaining them.
	//
	// Deliberately portable SQL: Migrate also runs against SQLite in tests, so
	// this avoids FILTER (WHERE ...) and IS DISTINCT FROM.
	if db.Migrator().HasTable("tracked_releases") && db.Migrator().HasTable("monitored_artists") {
		if err := db.Exec(`
			UPDATE monitored_artists SET
				total_releases = (
					SELECT count(*) FROM tracked_releases tr
					WHERE tr.artist_id = monitored_artists.id
				),
				acquired_releases = (
					SELECT count(*) FROM tracked_releases tr
					WHERE tr.artist_id = monitored_artists.id AND tr.status = 'acquired'
				)
			WHERE coalesce(total_releases, 0) <> (
					SELECT count(*) FROM tracked_releases tr
					WHERE tr.artist_id = monitored_artists.id
				)
			   OR coalesce(acquired_releases, 0) <> (
					SELECT count(*) FROM tracked_releases tr
					WHERE tr.artist_id = monitored_artists.id AND tr.status = 'acquired'
				)`).Error; err != nil {
			return fmt.Errorf("failed to backfill artist release counters: %w", err)
		}
	}

	// Backfill acoustics: every acquisition claims an AcoustID score of 0 and
	// not one of them was ever scored. The column was a plain int, so "the
	// lookup never ran" and "the lookup scored zero" were the same stored
	// value, and no fpcalc in the image meant nothing was ever fingerprinted -
	// 144 rows reading as a real, permanently-empty score.
	//
	// AcoustIDScore is a nullable pointer now, and every zero predating a
	// working fpcalc is genuinely unscored. A lookup that returns a result at
	// all returns one above zero confidence, but the stored integer is
	// truncated, so a very low confidence can still land on 0 - which is
	// exactly the value the acceptance criteria want to keep distinct from
	// "never asked". So this runs once, behind a marker, and a zero written
	// afterwards by a working lookup is left alone.
	//
	// The marker check, the backfill and the marker write are one transaction.
	// Run as separate statements they leave a window in which the marker is absent
	// while the UPDATE has already run: a second process booting concurrently (the
	// worker starts on its own schedule, and `database.Migrate` only runs in the
	// server) would repeat the backfill, or a real low-confidence zero committed
	// mid-window would be nulled with nothing left to restore it. Committing them
	// together means the marker exists exactly when the backfill has been applied,
	// so "the backfill ran once" is a fact rather than a hopeful pair of writes.
	if db.Migrator().HasTable("acquisitions") {
		const marker = "acoustid_unscored_backfill_v1"
		if err := db.Transaction(func(tx *gorm.DB) error {
			var done int64
			if err := tx.Table("settings").Where("key = ?", marker).Count(&done).Error; err != nil {
				return fmt.Errorf("failed to read %s marker: %w", marker, err)
			}
			if done != 0 {
				return nil
			}
			if err := tx.Exec(
				`UPDATE acquisitions SET acoustid_score = NULL WHERE acoustid_score = 0`,
			).Error; err != nil {
				return fmt.Errorf("failed to backfill unscored acoustid scores: %w", err)
			}
			if err := tx.Exec(
				`INSERT INTO settings (key, value, type, updated_at) VALUES (?, ?, ?, ?)
				 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
				marker, "1", "string", time.Now(),
			).Error; err != nil {
				return fmt.Errorf("failed to record %s marker: %w", marker, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

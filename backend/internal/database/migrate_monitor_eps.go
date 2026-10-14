package database

import (
	"fmt"

	"gorm.io/gorm"
)

// monitorEPsLegacyColumn is the name GORM derived for MonitoredArtist.MonitorEPs
// before the field carried an explicit `column:` tag. GORM's naming strategy
// reads the field as monitor_e_ps -- it does not treat "EP" as an initialism --
// while the SQL bootstrap in
// ops/db/init/migrations/2026_03_04_006_artist_tracking.sql creates monitor_eps.
const monitorEPsLegacyColumn = "monitor_e_ps"

// monitorEPsColumn is the name both schema sources now agree on: the one the SQL
// bootstrap creates, and the one MonitoredArtist.MonitorEPs resolves to through
// its `gorm:"column:monitor_eps"` tag.
const monitorEPsColumn = "monitor_eps"

// reconcileMonitorEPsColumn renames a legacy monitor_e_ps column on
// monitored_artists to monitor_eps, and collapses the pair if a database somehow
// carries both.
//
// It runs before AutoMigrate, because AutoMigrate is additive. Given a model
// whose field resolves to monitor_eps and a table carrying monitor_e_ps, it
// simply adds a second column: the row is then split in two, every write through
// GORM lands in the column the SQL bootstrap never named, and that column stays
// at its NOT NULL DEFAULT TRUE forever, which reads as "this deployment monitors
// EPs" no matter what the operator chose.
//
// Where both columns exist the legacy one wins. Every database that ran
// AutoMigrate has been *writing* monitor_e_ps, because that is where GORM mapped
// the field, so its value is the live one and the column the bootstrap created is
// the stale copy. The copy is carried across before the legacy column is dropped.
//
// Portability: RENAME COLUMN needs SQLite >= 3.25 and DROP COLUMN >= 3.35, and
// this package's driver bundles a far newer build, so there is no dialect branch.
func reconcileMonitorEPsColumn(db *gorm.DB) error {
	if !db.Migrator().HasTable("monitored_artists") {
		return nil
	}
	if !db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn) {
		// The common case, including every database this repository's tests build:
		// nothing to reconcile.
		return nil
	}

	if !db.Migrator().HasColumn("monitored_artists", monitorEPsColumn) {
		if err := db.Exec(
			`ALTER TABLE monitored_artists RENAME COLUMN ` + monitorEPsLegacyColumn + ` TO ` + monitorEPsColumn,
		).Error; err != nil {
			return fmt.Errorf("failed to rename monitored_artists.%s to %s: %w", monitorEPsLegacyColumn, monitorEPsColumn, err)
		}
		return nil
	}

	if err := db.Exec(
		`UPDATE monitored_artists SET ` + monitorEPsColumn + ` = ` + monitorEPsLegacyColumn,
	).Error; err != nil {
		return fmt.Errorf("failed to copy monitored_artists.%s onto %s: %w", monitorEPsLegacyColumn, monitorEPsColumn, err)
	}
	if err := db.Exec(
		`ALTER TABLE monitored_artists DROP COLUMN ` + monitorEPsLegacyColumn,
	).Error; err != nil {
		return fmt.Errorf("failed to drop the legacy monitored_artists.%s column: %w", monitorEPsLegacyColumn, err)
	}
	return nil
}

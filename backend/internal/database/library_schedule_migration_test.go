package database

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// migrationLibraryScheduleSQL is the body of
// ops/db/init/migrations/2026_10_12_001_schedule_library_scope.sql, duplicated
// here deliberately: a test that reads the file proves the file parses, not
// that these statements do what the migration claims on the driver that runs in
// production. If the migration changes, this must change with it.
const migrationLibraryScheduleSQL = `
ALTER TABLE IF EXISTS schedules ADD COLUMN IF NOT EXISTS job_type TEXT NOT NULL DEFAULT 'sync';
ALTER TABLE IF EXISTS schedules ADD COLUMN IF NOT EXISTS library_id UUID;
ALTER TABLE IF EXISTS schedules ALTER COLUMN watchlist_id DROP NOT NULL;
UPDATE schedules SET job_type = 'sync' WHERE job_type IS NULL OR job_type = '';`

// TestLibraryScheduleMigration_Postgres proves the migration lands on Postgres,
// the driver production actually runs. It works on a scratch table carrying the
// pre-migration shape rather than on `schedules` itself, because the interesting
// assertion is an INSERT that must now succeed -- and the real table's
// library_id carries a foreign key a fixture would have to satisfy.
//
// **Skipped, not passed, when DATABASE_URL is absent.** Postgres is where the
// ALTER runs: SQLite has no ALTER COLUMN and gets the change from AutoMigrate's
// table rebuild instead, so a green result here that never executed is the
// failure this test exists to prevent.
func TestLibraryScheduleMigration_Postgres(t *testing.T) {
	db := setupPostgresForMigration(t)

	const scratch = "zz_migration_schedule_scratch"
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+scratch).Error)
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + scratch) })

	// The shape a deployed database had before this change: a watchlist-scoped
	// schedule, NOT NULL, with no job type at all.
	require.NoError(t, db.Exec("CREATE TABLE "+scratch+` (
		id SERIAL PRIMARY KEY,
		watchlist_id UUID NOT NULL,
		cron_expr TEXT NOT NULL,
		timezone TEXT NOT NULL DEFAULT 'UTC',
		enabled BOOLEAN NOT NULL
	)`).Error)
	preexisting := uuid.New()
	require.NoError(t, db.Exec(
		"INSERT INTO "+scratch+" (watchlist_id, cron_expr, enabled) VALUES (?, ?, ?)",
		preexisting, "0 3 * * *", true).Error)

	// Twice. Every file in the migrations directory promises to be re-runnable,
	// and an operator applying one by hand must not have to check first.
	for i := 1; i <= 2; i++ {
		require.NoError(t,
			db.Exec(strings.ReplaceAll(migrationLibraryScheduleSQL, "schedules", scratch)).Error,
			"migration run %d failed on postgres", i)
	}

	require.True(t, db.Migrator().HasColumn(scratch, "job_type"), "job_type column missing")
	require.True(t, db.Migrator().HasColumn(scratch, "library_id"), "library_id column missing")

	// A row that predates the migration is a watchlist sync -- not an empty job
	// type, which the scheduler would have to guess about at 30-second intervals.
	var jobType string
	require.NoError(t, db.Table(scratch).Select("job_type").
		Where("watchlist_id = ?", preexisting).Row().Scan(&jobType))
	require.Equal(t, "sync", jobType)

	// The acceptance criterion: a library schedule is a row with no watchlist.
	// Before the ALTER this INSERT was a NOT NULL violation, and the constraint
	// would have named a column nobody set.
	libraryID := uuid.New()
	require.NoError(t, db.Exec(
		"INSERT INTO "+scratch+" (watchlist_id, library_id, job_type, cron_expr, enabled) VALUES (NULL, ?, 'scan', '0 4 * * *', true)",
		libraryID).Error)

	var stored *uuid.UUID
	require.NoError(t, db.Table(scratch).Select("library_id").
		Where("cron_expr = ?", "0 4 * * *").Row().Scan(&stored))
	require.NotNil(t, stored)
	require.Equal(t, libraryID, *stored)

	// The rows that were there before are still there: ADD COLUMN and DROP
	// NOT NULL must not have rewritten the table away.
	var count int64
	require.NoError(t, db.Table(scratch).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

// TestLibraryScheduleMigration_RealTableAcceptsLibraryScope asserts the same
// shape on the table production actually writes to, because a scratch table
// proves the statements work and not that they were ever applied here. The
// inserted row is rolled back: this database is shared with the rest of the
// gated suite.
func TestLibraryScheduleMigration_RealTableAcceptsLibraryScope(t *testing.T) {
	db := setupPostgresForMigration(t)
	if !db.Migrator().HasTable("schedules") {
		t.Skip("schedules does not exist in this database")
	}

	require.NoError(t, db.Exec(migrationLibraryScheduleSQL).Error)

	var nullable bool
	require.NoError(t, db.Raw(`SELECT is_nullable = 'YES' FROM information_schema.columns
		WHERE table_name = 'schedules' AND column_name = 'watchlist_id'`).Scan(&nullable).Error)
	require.True(t, nullable, "watchlist_id must be nullable, or no library schedule can be stored")

	// information_schema could be right while a trigger or a CHECK still refuses
	// the row, so write one and roll it back.
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec(`INSERT INTO schedules
		(job_type, watchlist_id, library_id, cron_expr, timezone, enabled, created_at, updated_at)
		VALUES ('scan', NULL, NULL, '0 4 * * *', 'UTC', true, now(), now())`).Error,
		"a scan schedule with no watchlist must be storable -- that is the migration")
	require.NoError(t, tx.Rollback().Error)
}

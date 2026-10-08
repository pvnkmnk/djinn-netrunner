package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// migrationBootstrapEnrolledAtSQL is the body of
// ops/db/init/migrations/2026_10_08_001_bootstrap_enrolled_at.sql, duplicated
// here deliberately: a test that reads the file proves the file parses, not that
// these statements do what the migration claims on the driver that runs in
// production. Same shape as artist_provenance_migration_test.go, and if the
// migration changes this must change with it.
const migrationBootstrapEnrolledAtSQL = `
ALTER TABLE users ADD COLUMN IF NOT EXISTS bootstrap_enrolled_at TIMESTAMPTZ;`

// TestBootstrapEnrolledAtMigration_Postgres proves the migration lands on
// Postgres, the driver production actually runs. SQLite is covered by
// AutoMigrate in the unit suite; asserting only one of the two and calling it
// done is how a driver-specific DDL bug reaches a deploy.
//
// Skipped, not passed, when DATABASE_URL is absent or unreachable. A green
// result here that never executed is the failure this test exists to prevent.
func TestBootstrapEnrolledAtMigration_Postgres(t *testing.T) {
	db := setupPostgresForMigration(t)

	if !db.Migrator().HasTable("users") {
		t.Skip("users does not exist in this database")
	}

	// Start from the pre-migration shape, or a database that already has the
	// column would pass without proving anything.
	if err := db.Exec("ALTER TABLE users DROP COLUMN IF EXISTS bootstrap_enrolled_at").Error; err != nil {
		t.Fatalf("could not reset to pre-migration shape: %v", err)
	}
	require.False(t, db.Migrator().HasColumn("users", "bootstrap_enrolled_at"))

	// A column that must survive, so ADD COLUMN is shown not to disturb its
	// neighbours rather than merely assumed not to.
	neighbourBefore := db.Migrator().HasColumn("users", "password_hash")
	require.True(t, neighbourBefore, "the users table should have password_hash")

	// Run it twice. The second run is the assertion that matters: every file in
	// this directory promises idempotency via IF NOT EXISTS, and an operator
	// re-running a migration by hand must not fail.
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Exec(migrationBootstrapEnrolledAtSQL).Error,
			"migration run %d failed on postgres", i)
	}

	require.True(t, db.Migrator().HasColumn("users", "bootstrap_enrolled_at"),
		"bootstrap_enrolled_at missing after migration")
	require.Equal(t, neighbourBefore, db.Migrator().HasColumn("users", "password_hash"),
		"ADD COLUMN must not disturb the columns around it")

	// Nullable with NO default is the whole point of the column: NULL means
	// "never proved anything", and a default would let a bare INSERT invent a
	// proof. That is a property of the DDL, not of the Go model, so it is
	// asserted here on the real column.
	var nullable, columnDefault string
	require.NoError(t, db.Raw(`
		SELECT is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_name = 'users' AND column_name = 'bootstrap_enrolled_at'`).
		Row().Scan(&nullable, &columnDefault))
	require.Equal(t, "YES", nullable,
		"the column must be nullable, or every existing row needs a fabricated value")
	require.Empty(t, columnDefault,
		"a default would let a bare INSERT invent an enrollment proof")
}

// TestBootstrapEnrolledAtMigration_PreservesExistingRows covers the second
// shape: a table that already holds rows. ADD COLUMN is documented not to
// rewrite the table, and a row written before the migration must read back with
// NULL rather than failing to load. A scratch table with the same shape stands
// in, matching artist_provenance_migration_test.go: fixtures that write to the
// real users table on a shared database are awkward to clean up and would make
// an unrelated failure look like this one.
func TestBootstrapEnrolledAtMigration_PreservesExistingRows(t *testing.T) {
	db := setupPostgresForMigration(t)

	const scratch = "zz_migration_bootstrap_enrolled_scratch"
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+scratch).Error)
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + scratch) })

	require.NoError(t, db.Exec(
		"CREATE TABLE " + scratch + " (id SERIAL PRIMARY KEY, email TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec(
		"INSERT INTO " + scratch + " (email) VALUES ('a@example.com'), ('b@example.com')").Error)

	var before int64
	require.NoError(t, db.Table(scratch).Count(&before).Error)
	require.EqualValues(t, 2, before)

	require.NoError(t, db.Exec(
		"ALTER TABLE " + scratch + " ADD COLUMN IF NOT EXISTS bootstrap_enrolled_at TIMESTAMPTZ").Error)

	var after int64
	require.NoError(t, db.Table(scratch).Count(&after).Error)
	require.Equal(t, before, after, "adding the column must not lose rows")

	// The migration itself changes nothing about what already exists: every
	// pre-existing row reads as "never proved anything", which is the value the
	// boot gate treats as not-enrolled.
	var enrolled *string
	require.NoError(t, db.Table(scratch).Select("bootstrap_enrolled_at").
		Where("email = ?", "a@example.com").
		Row().Scan(&enrolled))
	require.Nil(t, enrolled,
		"a row that predates the migration must read NULL, not a fabricated timestamp")
}

package database

import (
	"os"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupPostgresForMigration connects using DATABASE_URL, skipping when absent
// (unit CI has no Postgres) or unreachable.
//
// Deliberately the same shape as setupPostgresForLocks: a helper per concern
// would be two owners of "connect to Postgres if you can", and they would drift
// on the skip conditions — which is exactly how a migration test starts
// reporting green because it silently skipped.
func setupPostgresForMigration(t *testing.T) *gorm.DB {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := Connect(&config.Config{DatabaseURL: dbURL})
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}
	if db.Dialector.Name() != "postgres" {
		t.Skip("DATABASE_URL is not postgres")
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return db
}

// migrationArtistProvenanceSQL is the body of
// ops/db/init/migrations/2026_10_07_001_artist_provenance.sql, duplicated here
// deliberately: a test that reads the file proves the file parses, not that
// these statements do what the migration claims on the driver that runs in
// production. If the migration changes, this must change with it.
const migrationArtistProvenanceSQL = `
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS country TEXT;
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS artist_type TEXT;`

// TestArtistProvenanceMigration_Postgres proves the migration lands on
// Postgres, the driver production actually runs. SQLite is covered by
// AutoMigrate in the unit suite; asserting only one of the two and calling it
// done is how a driver-specific DDL bug reaches a deploy.
//
// **Skipped, not passed, when DATABASE_URL is absent.** A green result here
// that never executed is the failure this test exists to prevent.
func TestArtistProvenanceMigration_Postgres(t *testing.T) {
	db := setupPostgresForMigration(t)

	if !db.Migrator().HasTable("monitored_artists") {
		t.Skip("monitored_artists does not exist in this database")
	}

	// Start from the pre-migration shape: both columns absent. A database that
	// has already been migrated would otherwise pass without proving anything.
	if err := db.Exec("ALTER TABLE monitored_artists DROP COLUMN IF EXISTS country").Error; err != nil {
		t.Fatalf("could not reset to pre-migration shape: %v", err)
	}
	if err := db.Exec("ALTER TABLE monitored_artists DROP COLUMN IF EXISTS artist_type").Error; err != nil {
		t.Fatalf("could not reset to pre-migration shape: %v", err)
	}
	require.False(t, db.Migrator().HasColumn("monitored_artists", "country"))
	require.False(t, db.Migrator().HasColumn("monitored_artists", "artist_type"))

	before := db.Migrator().HasColumn("monitored_artists", "name")

	// Run the migration twice. The second run is the assertion that matters:
	// every file in this directory promises idempotency via IF NOT EXISTS, and
	// an operator re-running a migration by hand must not fail.
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Exec(migrationArtistProvenanceSQL).Error,
			"migration run %d failed on postgres", i)
	}

	require.True(t, db.Migrator().HasColumn("monitored_artists", "country"),
		"country column missing after migration")
	require.True(t, db.Migrator().HasColumn("monitored_artists", "artist_type"),
		"artist_type column missing after migration")
	require.Equal(t, before, db.Migrator().HasColumn("monitored_artists", "name"),
		"ADD COLUMN must not disturb the columns around it")
}

// TestArtistProvenanceMigration_PreservesExistingRows covers the second shape:
// a table that already holds rows. ADD COLUMN is documented not to rewrite the
// table, and this is the assertion a reader of the migration would want but
// cannot take on faith — a table with a foreign key is awkward to fixture
// against a shared database, so a scratch table with the same shape stands in.
func TestArtistProvenanceMigration_PreservesExistingRows(t *testing.T) {
	db := setupPostgresForMigration(t)

	const scratch = "zz_migration_provenance_scratch"
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+scratch).Error)
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + scratch) })

	require.NoError(t, db.Exec(
		"CREATE TABLE "+scratch+" (id SERIAL PRIMARY KEY, music_brainz_id TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec(
		"INSERT INTO "+scratch+" (music_brainz_id) VALUES ('mbid-a'), ('mbid-b')").Error)

	var before int64
	require.NoError(t, db.Table(scratch).Count(&before).Error)
	require.EqualValues(t, 2, before)

	require.NoError(t, db.Exec(
		"ALTER TABLE "+scratch+" ADD COLUMN IF NOT EXISTS country TEXT").Error)
	require.NoError(t, db.Exec(
		"ALTER TABLE "+scratch+" ADD COLUMN IF NOT EXISTS artist_type TEXT").Error)

	var after int64
	require.NoError(t, db.Table(scratch).Count(&after).Error)
	require.Equal(t, before, after, "adding provenance columns must not lose rows")

	// The new columns are additive and nullable, so a row written before the
	// migration reads back with them empty rather than failing to load.
	var country, artistType *string
	require.NoError(t, db.Table(scratch).Select("country, artist_type").
		Where("music_brainz_id = ?", "mbid-a").
		Row().Scan(&country, &artistType))
	require.Nil(t, country)
	require.Nil(t, artistType)
}
package database

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migrationMonitorEPsColumnSQL is the body of
// ops/db/init/migrations/2026_10_08_002_reconcile_monitor_eps_column.sql,
// duplicated here deliberately: a test that reads the file proves the file
// parses, not that these statements do what the migration claims on the driver
// that runs in production. Same shape as artist_provenance_migration_test.go and
// bootstrap_enrolled_at_migration_test.go, and if the migration changes this must
// change with it.
const migrationMonitorEPsColumnSQL = `
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'monitored_artists' AND column_name = 'monitor_e_ps'
    ) THEN
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'monitored_artists' AND column_name = 'monitor_eps'
        ) THEN
            UPDATE monitored_artists SET monitor_eps = monitor_e_ps;
            ALTER TABLE monitored_artists DROP COLUMN monitor_e_ps;
        ELSE
            ALTER TABLE monitored_artists RENAME COLUMN monitor_e_ps TO monitor_eps;
        END IF;
    END IF;
END $$;

COMMENT ON COLUMN monitored_artists.monitor_eps IS
    'Whether this artist''s EPs are monitored; the Go model tags MonitorEPs with this name, so the two schema sources agree (DJI-618)';`

// monitorEPsCanonical opens a transaction on the database the gate primed, with
// monitored_artists in the canonical shape: monitor_eps and no monitor_e_ps.
// Each test then builds the drift it is about, so the shared database is not
// expected to be in any particular state.
//
// A transaction rather than a scratch table. The migration is a DO block naming
// monitored_artists, so retargeting it at a scratch table would execute a
// different statement than the one an operator runs - and unlike the ADD COLUMN
// migrations already covered here, this one drops a column, which on a shared
// database would destroy state a sibling test may still be reading. Postgres
// applies DDL transactionally, so the drifted shape and the repair are both
// observable inside the transaction and both vanish when it rolls back.
func monitorEPsCanonical(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupPostgresForMigration(t)
	if !db.Migrator().HasTable("monitored_artists") {
		t.Skip("monitored_artists does not exist in this database")
	}

	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })

	require.NoError(t, tx.Exec("ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS "+monitorEPsColumn+" BOOLEAN NOT NULL DEFAULT TRUE").Error)
	require.NoError(t, tx.Exec("ALTER TABLE monitored_artists DROP COLUMN IF EXISTS "+monitorEPsLegacyColumn).Error)
	require.True(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
	return tx
}

// monitorEPsScratchArtist inserts one artist row, with a profile created in the
// same transaction to satisfy the foreign key. Both are rolled back with the
// transaction, so a shared database is left exactly as it was.
func monitorEPsScratchArtist(t *testing.T, tx *gorm.DB, monitorEPs bool) uuid.UUID {
	t.Helper()
	profile := QualityProfile{Name: "monitor eps scratch " + uuid.NewString()}
	require.NoError(t, tx.Create(&profile).Error)
	artist := MonitoredArtist{
		ID:               uuid.New(),
		MusicBrainzID:    "mbid-monitor-eps-scratch-" + uuid.NewString(),
		Name:             "Monitor EPs scratch",
		QualityProfileID: profile.ID,
		MonitorEPs:       monitorEPs,
	}
	require.NoError(t, tx.Create(&artist).Error)

	// Written explicitly, not left to Create: MonitorEPs carries a column default,
	// and GORM omits a zero value for a field that has one -- so Create with false
	// leaves the database default TRUE in place and the row would not hold the
	// value this helper claims it does. Measured on Postgres, on the first run of
	// this test (the rename case read its artist back as monitored).
	require.NoError(t, tx.Model(&MonitoredArtist{}).
		Where("id = ?", artist.ID).Update("MonitorEPs", monitorEPs).Error)
	return artist.ID
}

// TestMonitorEPsMigration_Postgres is the shape a real deployment is in: the
// bootstrap created monitor_eps, an older binary's AutoMigrate added
// monitor_e_ps beside it, and every write since has gone to the latter. It proves
// the migration lands on the production driver, collapses the pair, and carries
// the live value onto the column that survives.
//
// Skipped, not passed, when DATABASE_URL is absent or unreachable: a green result
// here that never executed is the failure this test exists to prevent, which is
// also why it is listed in scripts/postgres_gate.py's REQUIRED set.
func TestMonitorEPsMigration_Postgres(t *testing.T) {
	tx := monitorEPsCanonical(t)

	// The row GORM wrote: monitor_e_ps, the legacy column, is the live value. The
	// bootstrap's monitor_eps is left at its NOT NULL DEFAULT TRUE, which is the
	// stale copy an operator would otherwise keep reading.
	id := monitorEPsScratchArtist(t, tx, true)
	require.NoError(t, tx.Exec("ALTER TABLE monitored_artists ADD COLUMN "+monitorEPsLegacyColumn+" BOOLEAN NOT NULL DEFAULT TRUE").Error)
	require.NoError(t, tx.Exec("UPDATE monitored_artists SET "+monitorEPsLegacyColumn+" = FALSE WHERE id = ?", id).Error)

	// Run it twice. The second run is the assertion that matters: an operator
	// re-applying a migration by hand must not fail, and the block above is
	// written to be a no-op the second time.
	for i := 1; i <= 2; i++ {
		require.NoError(t, tx.Exec(migrationMonitorEPsColumnSQL).Error,
			"migration run %d failed on postgres", i)
	}

	assert.False(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"a deployment must not be left holding both columns")
	assert.True(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
	// A neighbour named so it exists in the runtime schema. GORM derives
	// `music_brainz_id` for MonitoredArtist.MusicBrainzID where the SQL bootstrap
	// writes `musicbrainz_id` -- the same class of drift as this migration
	// repairs, on a different column, and not this migration's business. Asserting
	// on the bootstrap's spelling here fails against a database the migrator built
	// (measured 2026-10-14); `name` is a column both sources agree on.
	assert.True(t, tx.Migrator().HasColumn("monitored_artists", "name"),
		"the repair must not disturb the columns around it")

	// Read through the model, which is the reader the fix is for: the value the
	// application wrote has to be the one that survives.
	var got MonitoredArtist
	require.NoError(t, tx.First(&got, "id = ?", id).Error)
	assert.False(t, got.MonitorEPs,
		"the GORM-written value has to win: monitor_eps held the stale bootstrap default")

	// The file's COMMENT lands too, which is the cheapest proof that the whole
	// body ran rather than the DO block alone.
	var comment string
	require.NoError(t, tx.Raw(`
		SELECT COALESCE(col_description('monitored_artists'::regclass, ordinal_position), '')
		FROM information_schema.columns
		WHERE table_name = 'monitored_artists' AND column_name = $1`, monitorEPsColumn).Scan(&comment).Error)
	assert.NotEmpty(t, comment, "the migration's COMMENT ON COLUMN did not land")
}

// TestMonitorEPsMigration_RenamesTheLegacyColumn is the other drifted shape: only
// the legacy column exists, as a database written entirely by an older binary
// has it. The value has to travel with the rename - a repair that dropped and
// re-added the column would leave every artist at the default.
func TestMonitorEPsMigration_RenamesTheLegacyColumn(t *testing.T) {
	tx := monitorEPsCanonical(t)

	id := monitorEPsScratchArtist(t, tx, false)
	// Take the canonical column away under the row, so the only spelling left is
	// the legacy one, holding the value GORM wrote.
	require.NoError(t, tx.Exec("ALTER TABLE monitored_artists RENAME COLUMN "+monitorEPsColumn+" TO "+monitorEPsLegacyColumn).Error)
	require.False(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsColumn))

	for i := 1; i <= 2; i++ {
		require.NoError(t, tx.Exec(migrationMonitorEPsColumnSQL).Error,
			"migration run %d failed on postgres", i)
	}

	assert.True(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsColumn),
		"the rename did not happen")
	assert.False(t, tx.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"renamed, not shadowed by a second column")

	var got MonitoredArtist
	require.NoError(t, tx.First(&got, "id = ?", id).Error)
	assert.False(t, got.MonitorEPs, "the value has to travel with the column")
}

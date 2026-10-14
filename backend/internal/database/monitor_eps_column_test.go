package database

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// monitoredArtistColumnsFromSQL reads the column names out of the CREATE TABLE
// block for monitored_artists in the SQL bootstrap migration.
//
// The file is parsed rather than the names being restated in a constant here,
// because a constant would agree with the model and say nothing at all about the
// SQL - which is precisely how the two sources drifted apart.
func monitoredArtistColumnsFromSQL(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the SQL bootstrap is half of this contract, so a move is a change to the contract")

	body := string(raw)
	start := strings.Index(body, "CREATE TABLE IF NOT EXISTS monitored_artists")
	require.GreaterOrEqual(t, start, 0, "this migration creates monitored_artists; a rename here means the reader below is stale")
	body = body[start:]

	end := strings.Index(body, "\n);")
	require.GreaterOrEqual(t, end, 0, "unterminated CREATE TABLE block")

	var columns []string
	for _, line := range strings.Split(body[:end], "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "CREATE TABLE") {
			continue
		}
		// Table-level constraints are not columns, and the constraint names in
		// this block (valid_total_releases and friends) would otherwise be read
		// as three extra columns.
		if strings.HasPrefix(line, "CONSTRAINT ") || strings.HasPrefix(line, "UNIQUE") ||
			strings.HasPrefix(line, "PRIMARY ") || strings.HasPrefix(line, "FOREIGN ") {
			continue
		}
		columns = append(columns, strings.Fields(line)[0])
	}
	require.NotEmpty(t, columns, "no columns parsed: the reader, not the migration, is the thing to check")
	return columns
}

// TestMonitoredArtist_MonitorEPsUsesTheBootstrapColumnName is DJI-635's guard on
// the name itself: the Go model and the SQL bootstrap have to resolve to the same
// column, and this is the half that never reached master with that ticket.
//
// Both directions are asserted against real sources. Without the `column:` tag
// GORM derives monitor_e_ps, which parses, migrates and passes every test that
// merely loads the model - so the assertion has to be on the resolved name, read
// off the schema rather than described in a comment, and on the name the SQL
// actually declares, read off the file.
func TestMonitoredArtist_MonitorEPsUsesTheBootstrapColumnName(t *testing.T) {
	parsed, err := schema.Parse(&MonitoredArtist{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	field := parsed.LookUpField("MonitorEPs")
	require.NotNil(t, field, "MonitorEPs is the field under test")

	assert.Equal(t, monitorEPsColumn, field.DBName,
		"the model must resolve to the column the SQL bootstrap creates, not the one GORM derives from the field name")

	columns := monitoredArtistColumnsFromSQL(t, filepath.Join(
		"..", "..", "..", "ops", "db", "init", "migrations", "2026_03_04_006_artist_tracking.sql"))
	assert.Contains(t, columns, monitorEPsColumn, "the SQL bootstrap creates this column")
	assert.NotContains(t, columns, monitorEPsLegacyColumn, "and never the name GORM derived")
}

// monitorEPsTestTable opens a database with a monitored_artists table shaped by
// whichever of the two column spellings are asked for - the table as a drifted
// deployment has it, not the model's own idea of the schema. Passing neither
// creates no table, which is the fresh-install case.
//
// File-backed, not :memory:: GORM pools its connections and every :memory:
// connection is a separate database, so a second connection reads "no such table"
// instead of the table just created.
func monitorEPsTestTable(t *testing.T, legacy, canonical bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "monitor_eps.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// Registered after t.TempDir's own cleanup, so it runs first: Windows refuses
	// to remove a directory whose database file is still open.
	t.Cleanup(func() { _ = sqlDB.Close() })

	if legacy || canonical {
		columns := []string{"id INTEGER PRIMARY KEY"}
		if legacy {
			columns = append(columns, monitorEPsLegacyColumn+" BOOLEAN NOT NULL DEFAULT TRUE")
		}
		if canonical {
			columns = append(columns, monitorEPsColumn+" BOOLEAN NOT NULL DEFAULT TRUE")
		}
		require.NoError(t, db.Exec("CREATE TABLE monitored_artists ("+strings.Join(columns, ", ")+")").Error)
	}
	return db
}

func TestReconcileMonitorEPsColumn_RenamesTheLegacyColumn(t *testing.T) {
	db := monitorEPsTestTable(t, true, false)
	require.NoError(t, db.Exec("INSERT INTO monitored_artists (id, "+monitorEPsLegacyColumn+") VALUES (1, FALSE)").Error)

	require.NoError(t, reconcileMonitorEPsColumn(db))

	assert.True(t, db.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
	assert.False(t, db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"renamed, not shadowed by a second column")

	// A rename that lost the data would satisfy both checks above.
	var monitored bool
	require.NoError(t, db.Raw("SELECT "+monitorEPsColumn+" FROM monitored_artists WHERE id = 1").Scan(&monitored).Error)
	assert.False(t, monitored, "the value has to travel with the column")
}

func TestReconcileMonitorEPsColumn_KeepsTheValueGormWroteWhenBothExist(t *testing.T) {
	db := monitorEPsTestTable(t, true, true)
	// monitor_eps is what the SQL bootstrap left at its DEFAULT TRUE; monitor_e_ps
	// is where GORM has been writing since AutoMigrate added it.
	require.NoError(t, db.Exec("INSERT INTO monitored_artists (id, "+monitorEPsLegacyColumn+") VALUES (1, FALSE)").Error)

	require.NoError(t, reconcileMonitorEPsColumn(db))

	assert.False(t, db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"a deployment must not be left holding both columns")
	var monitored bool
	require.NoError(t, db.Raw("SELECT "+monitorEPsColumn+" FROM monitored_artists WHERE id = 1").Scan(&monitored).Error)
	assert.False(t, monitored, "the column GORM was writing has to win: the other one is the stale copy")
}

func TestReconcileMonitorEPsColumn_IsANoOpWhenThereIsNothingToReconcile(t *testing.T) {
	for _, tc := range []struct {
		name      string
		legacy    bool
		canonical bool
	}{
		{"already canonical", false, true},
		{"no table at all", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := monitorEPsTestTable(t, tc.legacy, tc.canonical)

			require.NoError(t, reconcileMonitorEPsColumn(db))

			assert.False(t, db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn))
			if tc.canonical {
				assert.True(t, db.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
			}
		})
	}
}

// TestMigrate_NamesTheMonitorEPsColumnOnce pins the boot path end to end: a fresh
// database gets one column, under the name the SQL migration uses. A model that
// resolves correctly and a bootstrap that spells it differently would still leave
// two columns, so the two halves are pinned separately.
func TestMigrate_NamesTheMonitorEPsColumnOnce(t *testing.T) {
	db := monitorEPsTestTable(t, false, false)

	require.NoError(t, Migrate(db))

	assert.True(t, db.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
	assert.False(t, db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"AutoMigrate must not invent a second, differently spelled column")
}

// TestMigrate_RepairsADatabaseCarryingBothColumnNames covers the acceptance
// criterion that no deployment is left with two columns. The SQL migration only
// repairs databases bootstrapped from ops/db/init/, so a deployment that has only
// ever started the binaries depends entirely on this path.
func TestMigrate_RepairsADatabaseCarryingBothColumnNames(t *testing.T) {
	db := monitorEPsTestTable(t, false, false)
	require.NoError(t, Migrate(db))
	require.True(t, db.Migrator().HasColumn("monitored_artists", monitorEPsColumn))

	// What an older binary left behind: AutoMigrate had added monitor_e_ps next to
	// the bootstrap's monitor_eps, and every GORM write went to the former.
	require.NoError(t, db.Exec("ALTER TABLE monitored_artists ADD COLUMN "+monitorEPsLegacyColumn+" BOOLEAN NOT NULL DEFAULT TRUE").Error)

	require.NoError(t, Migrate(db))

	assert.False(t, db.Migrator().HasColumn("monitored_artists", monitorEPsLegacyColumn),
		"the boot path has to collapse the pair, not leave it for the next reader to disambiguate")
	assert.True(t, db.Migrator().HasColumn("monitored_artists", monitorEPsColumn))
}

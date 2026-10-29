package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestUserDisablementMigration_Postgres proves the nullable account-disablement
// field is additive on the production database driver and that both enabled and
// disabled values round-trip through the model.
func TestUserDisablementMigration_Postgres(t *testing.T) {
	db := setupPostgresForMigration(t)

	if !db.Migrator().HasTable("users") {
		t.Skip("users does not exist in this database")
	}

	// Postgres DDL is transactional, so running the drop inside a transaction
	// that is rolled back returns the column *and its stored values*. The
	// column holds the only record that an account is disabled: re-adding it
	// outside the transaction leaves those rows NULL, which silently re-enables
	// every disabled account in the target database.
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })
	db = tx

	require.NoError(t, db.Exec("ALTER TABLE users DROP COLUMN IF EXISTS disabled_at").Error)
	require.False(t, db.Migrator().HasColumn("users", "disabled_at"))
	require.NoError(t, db.AutoMigrate(&User{}))
	require.True(t, db.Migrator().HasColumn("users", "disabled_at"))

	var nullable string
	var columnDefault string
	require.NoError(t, db.Raw(`
		SELECT is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_name = 'users' AND column_name = 'disabled_at'`).Row().Scan(&nullable, &columnDefault))
	require.Equal(t, "YES", nullable)
	require.Empty(t, columnDefault)

	email := fmt.Sprintf("disablement-migration-%d@example.com", time.Now().UnixNano())
	user := User{Email: email, PasswordHash: "migration-hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	t.Cleanup(func() { db.Delete(&User{}, user.ID) })

	var enabled User
	require.NoError(t, db.First(&enabled, user.ID).Error)
	require.Nil(t, enabled.DisabledAt)

	disabledAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, db.Model(&user).Update("disabled_at", disabledAt).Error)

	var disabled User
	require.NoError(t, db.First(&disabled, user.ID).Error)
	require.NotNil(t, disabled.DisabledAt)
	require.WithinDuration(t, disabledAt, *disabled.DisabledAt, time.Microsecond)
}

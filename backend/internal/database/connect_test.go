package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConnect_SQLiteMemory(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL: ":memory:",
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)

	// Verify connection works
	sqlDB, err := db.DB()
	require.NoError(t, err)
	err = sqlDB.Ping()
	assert.NoError(t, err)

	// Verify WAL mode is set (memory mode uses "memory" not "wal")
	var walMode string
	db.Raw("PRAGMA journal_mode").Scan(&walMode)
	// In-memory database uses "memory" mode
	assert.Equal(t, "memory", walMode)

	sqlDB.Close()
}

func TestConnect_SQLiteFile(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL: t.TempDir() + "/test_connect.db",
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)

	// Verify connection works
	sqlDB, err := db.DB()
	require.NoError(t, err)
	err = sqlDB.Ping()
	assert.NoError(t, err)

	// Clean up
	db.Exec("PRAGMA optimize")
	sqlDB.Close()
}

func TestConnect_SQLiteFileWithoutDbSuffix(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL: t.TempDir() + "/test_no_suffix",
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.Close()
}

func TestConnect_InvalidPostgres(t *testing.T) {
	// This test verifies graceful failure with invalid Postgres URL
	cfg := &config.Config{
		DatabaseURL: "postgres://nonexistent:5432/nonexistent",
	}

	db, err := Connect(cfg)
	// Should fail with connection error (DNS lookup failure on Windows)
	if err == nil {
		t.Fatal("Expected error for invalid Postgres connection")
	}
	assert.Nil(t, db)
}

func TestNewLockManager_ReturnType(t *testing.T) {
	// Test that NewLockManager returns correct types
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Lock{}))

	// SQLite should return TableLockManager
	lm := NewLockManager(db)
	assert.IsType(t, &TableLockManager{}, lm)
}

func TestTableLockManager_Close(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Lock{}))

	lm := &TableLockManager{db: db}
	err = lm.Close()
	assert.NoError(t, err)
}

func TestPostgresLockManager_Close(t *testing.T) {
	lm := &PostgresLockManager{}
	err := lm.Close()
	assert.NoError(t, err)
}

func TestPostgresLockManager_GetScopeLockKey_Consistency(t *testing.T) {
	lm := &PostgresLockManager{}

	// Same inputs should produce same output
	key1, err := lm.GetScopeLockKey(nil, "library", "uuid-abc")
	require.NoError(t, err)
	key2, err := lm.GetScopeLockKey(nil, "library", "uuid-abc")
	require.NoError(t, err)
	assert.Equal(t, key1, key2)

	// Different scope type should produce different key
	key3, err := lm.GetScopeLockKey(nil, "watchlist", "uuid-abc")
	require.NoError(t, err)
	assert.NotEqual(t, key1, key3)

	// Different scope ID should produce different key
	key4, err := lm.GetScopeLockKey(nil, "library", "uuid-xyz")
	require.NoError(t, err)
	assert.NotEqual(t, key1, key4)

	// Empty strings should still produce valid key
	key5, err := lm.GetScopeLockKey(nil, "", "")
	require.NoError(t, err)
	assert.NotZero(t, key5)
}

func TestTableLockManager_GetScopeLockKey_Consistency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Lock{}))

	lm := &TableLockManager{db: db}

	// Same inputs should produce same output
	key1, err := lm.GetScopeLockKey(nil, "artist", "mbid-123")
	require.NoError(t, err)
	key2, err := lm.GetScopeLockKey(nil, "artist", "mbid-123")
	require.NoError(t, err)
	assert.Equal(t, key1, key2)

	// Different scope type should produce different key
	key3, err := lm.GetScopeLockKey(nil, "album", "mbid-123")
	require.NoError(t, err)
	assert.NotEqual(t, key1, key3)
}

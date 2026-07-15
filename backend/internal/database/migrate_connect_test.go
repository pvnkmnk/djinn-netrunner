package database

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrate_SQLiteCreatesAllTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = Migrate(db)
	require.NoError(t, err)

	// Verify all expected tables exist
	tables := []string{
		"users", "sessions", "quality_profiles", "monitored_artists",
		"tracked_releases", "watchlists", "spotify_tokens", "jobs",
		"jobitems", "acquisitions", "libraries", "tracks", "playlists",
		"playlist_tracks", "schedules", "metadata_caches", "locks",
		"settings", "audit_logs", "peer_reputations",
	}

	for _, table := range tables {
		assert.True(t, db.Migrator().HasTable(table), "Table %s should exist", table)
	}
}

func TestMigrate_SQLiteIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// First migration
	err = Migrate(db)
	require.NoError(t, err)

	// Second migration should also succeed (idempotent)
	err = Migrate(db)
	require.NoError(t, err)

	// Tables should still exist
	assert.True(t, db.Migrator().HasTable("users"))
	assert.True(t, db.Migrator().HasTable("jobs"))
}

func TestMigrate_CreatesIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = Migrate(db)
	require.NoError(t, err)

	// Verify some indexes exist
	var count int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name LIKE '%email%'").Scan(&count)
	assert.GreaterOrEqual(t, count, int64(1), "Should have email index on users")
}

func TestConnect_WithValidPostgresURL(t *testing.T) {
	// Test that we can detect postgres URLs properly
	// We can't actually connect to postgres without a running instance,
	// but we can verify IsPostgres works
	tests := []struct {
		name     string
		url      string
		isPostgres bool
	}{
		{"postgres direct", "postgres://user:pass@host:5432/db", true},
		{"postgresql direct", "postgresql://user:pass@host:5432/db", true},
		{"sqlite file", "test.db", false},
		{"sqlite path", "./data/test.db", false},
		{"memory", ":memory:", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isPostgres, IsPostgres(tt.url))
		})
	}
}

func TestLockManager_InterfaceCompliance(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&Lock{})

	lm := NewLockManager(db)

	// Verify interface compliance
	var _ LockManager = lm
}

func TestTableLockManager_AcquireTryLock_WithContext(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&Lock{})

	lm := &TableLockManager{db: db}

	// Test with a simple key
	key := int64(12345)

	acquired, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.True(t, acquired)

	// Same key should not be acquired again
	acquired2, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.False(t, acquired2)

	// Release and re-acquire should work
	err = lm.ReleaseLock(nil, key)
	require.NoError(t, err)

	acquired3, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.True(t, acquired3)
}

func TestTableLockManager_MultipleKeys(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&Lock{})

	lm := &TableLockManager{db: db}

	keys := []int64{111, 222, 333, 444, 555}

	// Acquire all keys
	for _, key := range keys {
		acquired, err := lm.AcquireTryLock(nil, key)
		require.NoError(t, err)
		assert.True(t, acquired)
	}

	// All keys should be held
	for _, key := range keys {
		acquired, err := lm.AcquireTryLock(nil, key)
		require.NoError(t, err)
		assert.False(t, acquired)
	}

	// Release middle key
	err := lm.ReleaseLock(nil, keys[2])
	require.NoError(t, err)

	// Middle key should be acquirable again
	acquired, err := lm.AcquireTryLock(nil, keys[2])
	require.NoError(t, err)
	assert.True(t, acquired)
}

func TestMigrate_Postgres(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}

	// Run migration
	err = Migrate(db)
	require.NoError(t, err)

	// Verify all expected tables exist
	tables := []string{
		"users", "sessions", "quality_profiles", "monitored_artists",
		"tracked_releases", "watchlists", "spotify_tokens", "jobs",
		"jobitems", "acquisitions", "libraries", "tracks", "playlists",
		"playlist_tracks", "schedules", "metadata_caches", "locks",
		"settings", "audit_logs", "peer_reputations",
	}

	for _, table := range tables {
		assert.True(t, db.Migrator().HasTable(table), "Table %s should exist", table)
	}
}

func TestMigrate_PostgresIdempotent(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	if !IsPostgres(dbURL) {
		t.Skip("Test requires Postgres")
	}

	cfg := &config.Config{DatabaseURL: dbURL}
	db, err := Connect(cfg)
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}

	// First migration
	err = Migrate(db)
	require.NoError(t, err)

	// Second migration should also succeed (idempotent)
	err = Migrate(db)
	require.NoError(t, err)

	// Tables should still exist
	assert.True(t, db.Migrator().HasTable("users"))
	assert.True(t, db.Migrator().HasTable("jobs"))
}

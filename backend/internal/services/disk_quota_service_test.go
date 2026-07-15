package services

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/glebarez/sqlite"
)

func setupDiskQuotaDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	// Create tracks table manually (DiskQuotaService uses raw sql.DB, not GORM)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tracks (id INTEGER PRIMARY KEY, library_id TEXT, file_size INTEGER)`)
	require.NoError(t, err)

	return db
}

func TestDiskQuotaService_FormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes under 1KB", 512, "512 B"},
		{"exact 1KB", 1024, "1.0 KB"},
		{"1.5KB", 1536, "1.5 KB"},
		{"exact 1MB", 1048576, "1.0 MB"},
		{"exact 1GB", 1073741824, "1.0 GB"},
		{"exact 1TB", 1099511627776, "1.0 TB"},
		{"500 bytes", 500, "500 B"},
		{"100 bytes", 100, "100 B"},
		{"10KB", 10240, "10.0 KB"},
		{"100MB", 104857600, "100.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDiskQuotaService_CalculateLibraryUsage(t *testing.T) {
	db := setupDiskQuotaDB(t)
	svc := NewDiskQuotaService(db)
	libID := uuid.New().String()

	t.Run("empty library returns zero", func(t *testing.T) {
		usage, err := svc.CalculateLibraryUsage(libID)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), usage)
	})

	t.Run("sums track file sizes", func(t *testing.T) {
		// Insert test tracks
		tracks := []struct {
			libID string
			size  int64
		}{
			{libID, 1024},
			{libID, 2048},
			{libID, 3072},
		}
		for _, tr := range tracks {
			_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, tr.libID, tr.size)
			require.NoError(t, err)
		}

		usage, err := svc.CalculateLibraryUsage(libID)
		assert.NoError(t, err)
		assert.Equal(t, int64(6144), usage) // 1024 + 2048 + 3072
	})

	t.Run("ignores other library tracks", func(t *testing.T) {
		otherLibID := uuid.New().String()
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, otherLibID, 9999)
		require.NoError(t, err)

		usage, err := svc.CalculateLibraryUsage(libID)
		assert.NoError(t, err)
		assert.Equal(t, int64(6144), usage) // unchanged
	})
}

func TestDiskQuotaService_GetLibraryUsage(t *testing.T) {
	db := setupDiskQuotaDB(t)
	svc := NewDiskQuotaService(db)

	t.Run("per-library quota with MaxSizeBytes", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Test Library",
			Path: "/tmp/test",
		}
		maxSize := int64(1000)
		lib.MaxSizeBytes = &maxSize

		// Insert tracks totaling 500 bytes
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 300)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 200)
		require.NoError(t, err)

		usage, err := svc.GetLibraryUsage(lib)
		assert.NoError(t, err)
		assert.NotNil(t, usage)
		assert.Equal(t, libID.String(), usage.LibraryID)
		assert.Equal(t, "Test Library", usage.LibraryName)
		assert.Equal(t, int64(500), usage.UsedBytes)
		assert.Equal(t, int64(1000), usage.LimitBytes)
		assert.Equal(t, 50, usage.UsedPct) // 500/1000 * 100 = 50
	})

	t.Run("no tracks returns zero usage", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Empty Library",
			Path: "/tmp/empty",
		}
		maxSize := int64(500)
		lib.MaxSizeBytes = &maxSize

		usage, err := svc.GetLibraryUsage(lib)
		assert.NoError(t, err)
		assert.NotNil(t, usage)
		assert.Equal(t, int64(0), usage.UsedBytes)
		assert.Equal(t, int64(500), usage.LimitBytes)
		assert.Equal(t, 0, usage.UsedPct)
	})
}

func TestDiskQuotaService_CheckQuotaAlert(t *testing.T) {
	db := setupDiskQuotaDB(t)
	svc := NewDiskQuotaService(db)

	t.Run("returns usage when over default 80% threshold", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Near Quota",
			Path: "/tmp/near",
		}
		maxSize := int64(1000)
		lib.MaxSizeBytes = &maxSize

		// Insert 850 bytes (85% of 1000)
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 850)
		require.NoError(t, err)

		usage, err := svc.CheckQuotaAlert(lib)
		assert.NoError(t, err)
		assert.NotNil(t, usage, "should return usage when over 80% threshold")
		assert.Equal(t, 85, usage.UsedPct)
	})

	t.Run("returns nil when under default 80% threshold", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Under Quota",
			Path: "/tmp/under",
		}
		maxSize := int64(1000)
		lib.MaxSizeBytes = &maxSize

		// Insert 500 bytes (50% of 1000)
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 500)
		require.NoError(t, err)

		usage, err := svc.CheckQuotaAlert(lib)
		assert.NoError(t, err)
		assert.Nil(t, usage, "should return nil when under 80% threshold")
	})

	t.Run("custom quota alert threshold", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Custom Threshold",
			Path: "/tmp/custom",
		}
		maxSize := int64(1000)
		lib.MaxSizeBytes = &maxSize
		customThreshold := 50 // 50%
		lib.QuotaAlertAt = &customThreshold

		// Insert 600 bytes (60% of 1000) - over 50% threshold
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 600)
		require.NoError(t, err)

		usage, err := svc.CheckQuotaAlert(lib)
		assert.NoError(t, err)
		assert.NotNil(t, usage, "should return usage when over custom 50% threshold")
		assert.Equal(t, 60, usage.UsedPct)
	})

	t.Run("exactly at threshold returns usage", func(t *testing.T) {
		libID := uuid.New()
		lib := database.Library{
			ID:   libID,
			Name: "Exactly Threshold",
			Path: "/tmp/exact",
		}
		maxSize := int64(1000)
		lib.MaxSizeBytes = &maxSize

		// Insert 800 bytes (80% of 1000) - exactly at default threshold
		_, err := db.Exec(`INSERT INTO tracks (library_id, file_size) VALUES (?, ?)`, libID.String(), 800)
		require.NoError(t, err)

		usage, err := svc.CheckQuotaAlert(lib)
		assert.NoError(t, err)
		assert.NotNil(t, usage, "should return usage when at exactly 80% threshold")
	})
}

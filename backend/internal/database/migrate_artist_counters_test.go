package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-554: before the fix nothing ever wrote total_releases or
// acquired_releases, so every existing row sits at 0 while tracked_releases
// holds the real counts. Migrate repairs those rows once, so a user who
// upgrades does not have to wait for an unrelated scan or acquisition to make
// their artist list tell the truth.

func migrateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/migrate.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))

	// SQLite keeps the file locked until the handle is closed, and TempDir
	// cleanup runs after the test body - so close explicitly, or cleanup fails
	// with "being used by another process" and masks what the test meant.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	return db
}

func TestMigrate_BackfillsArtistReleaseCounters(t *testing.T) {
	db := migrateTestDB(t)

	artist := MonitoredArtist{
		ID:              uuid.New(),
		MusicBrainzID:   uuid.NewString(),
		Name:            "Backfill Artist",
		QualityProfileID: uuid.New(),
	}
	require.NoError(t, db.Create(&artist).Error)

	for i, status := range []string{"acquired", "acquired", "wanted", "queued", "failed"} {
		require.NoError(t, db.Create(&TrackedRelease{
			ID:             uuid.New(),
			ArtistID:       artist.ID,
			ReleaseGroupID: uuid.NewString() + string(rune('a'+i)),
			Title:          "Release",
			ReleaseType:    "Album",
			Status:         status,
		}).Error)
	}

	// The row is still at 0/0: the seed above never wrote the counters.
	var before MonitoredArtist
	require.NoError(t, db.First(&before, "id = ?", artist.ID).Error)
	require.Equal(t, 0, before.TotalReleases)
	require.Equal(t, 0, before.AcquiredReleases)

	// Running Migrate again is what a restart does.
	require.NoError(t, Migrate(db))

	var after MonitoredArtist
	require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)
	assert.Equal(t, 5, after.TotalReleases, "the backfill counts every tracked release")
	assert.Equal(t, 2, after.AcquiredReleases, "the backfill counts only acquired releases")
}

func TestMigrate_BackfillLeavesCorrectRowsAlone(t *testing.T) {
	db := migrateTestDB(t)

	artist := MonitoredArtist{
		ID:              uuid.New(),
		MusicBrainzID:   uuid.NewString(),
		Name:            "Already Correct",
		QualityProfileID: uuid.New(),
	}
	require.NoError(t, db.Create(&artist).Error)
	require.NoError(t, db.Create(&TrackedRelease{
		ID:             uuid.New(),
		ArtistID:       artist.ID,
		ReleaseGroupID: uuid.NewString(),
		Title:          "Release",
		ReleaseType:    "Album",
		Status:         "acquired",
	}).Error)
	require.NoError(t, Migrate(db))

	// A second Migrate must be a no-op for a row that already agrees, so the
	// fix costs nothing on every subsequent boot.
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))

	var artist2 MonitoredArtist
	require.NoError(t, db.First(&artist2, "id = ?", artist.ID).Error)
	assert.Equal(t, 1, artist2.TotalReleases)
	assert.Equal(t, 1, artist2.AcquiredReleases)
}

func TestMigrate_BackfillHandlesArtistWithNoReleases(t *testing.T) {
	db := migrateTestDB(t)

	artist := MonitoredArtist{
		ID:              uuid.New(),
		MusicBrainzID:   uuid.NewString(),
		Name:            "No Releases Yet",
		QualityProfileID: uuid.New(),
	}
	require.NoError(t, db.Create(&artist).Error)

	require.NoError(t, Migrate(db))

	var after MonitoredArtist
	require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)
	assert.Equal(t, 0, after.TotalReleases)
	assert.Equal(t, 0, after.AcquiredReleases)
}
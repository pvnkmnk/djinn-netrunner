package services

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-554: total_releases and acquired_releases were declared on
// MonitoredArtist and read by the artists list query, but nothing ever wrote
// them. Every artist therefore read "Releases: 0/0" forever, next to a
// Last Scan timestamp asserting the scan had run.
//
// These tests pin the two properties that make the write safe to re-run:
// it reflects the rows that exist rather than what a scan happened to return,
// and it is idempotent. Together those mean a rescan can never make the
// numbers worse.

func seedArtist(t *testing.T, db *gorm.DB) database.MonitoredArtist {
	t.Helper()
	artist := database.MonitoredArtist{
		ID:             uuid.New(),
		MusicBrainzID:  uuid.NewString(),
		Name:           "Counter Test Artist",
		SortName:       "Counter Test Artist",
		Monitored:      true,
		MonitorAlbums:  true,
		QualityProfileID: uuid.New(),
	}
	require.NoError(t, db.Create(&artist).Error)
	return artist
}

func addReleases(t *testing.T, db *gorm.DB, artistID uuid.UUID, statuses ...string) {
	t.Helper()
	for i, status := range statuses {
		rel := database.TrackedRelease{
			ID:             uuid.New(),
			ArtistID:       artistID,
			ReleaseGroupID: uuid.NewString(),
			Title:          "Release " + string(rune('A'+i)),
			ReleaseType:    "Album",
			Status:         status,
			Monitored:      true,
		}
		require.NoError(t, db.Create(&rel).Error)
	}
}

func countersOf(t *testing.T, db *gorm.DB, artistID uuid.UUID) (total int, acquired int) {
	t.Helper()
	var artist database.MonitoredArtist
	require.NoError(t, db.First(&artist, "id = ?", artistID).Error)
	return artist.TotalReleases, artist.AcquiredReleases
}

func TestRefreshArtistReleaseCounters_PopulatesFromTrackedReleases(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	artist := seedArtist(t, db)
	addReleases(t, db, artist.ID, "acquired", "acquired", "wanted", "queued")

	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))

	total, acquired := countersOf(t, db, artist.ID)
	assert.Equal(t, 4, total, "every tracked release counts toward the total")
	assert.Equal(t, 2, acquired, "only acquired releases count as acquired")
}

func TestRefreshArtistReleaseCounters_RescanCannotLowerTotals(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	artist := seedArtist(t, db)
	addReleases(t, db, artist.ID, "acquired", "acquired", "wanted", "wanted")
	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))

	total, acquired := countersOf(t, db, artist.ID)
	require.Equal(t, 4, total)
	require.Equal(t, 2, acquired)

	// A rescan that discovers nothing: nothing is created, nothing is deleted,
	// and nothing is passed in to say "the answer is zero". The counters are a
	// count of rows that still exist, so they must be unchanged. This is the
	// property that keeps a transient MusicBrainz failure — a rate limit, a
	// dropped connection, an artist with nothing published — from wiping a
	// real number off the artist's row.
	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))

	totalAfter, acquiredAfter := countersOf(t, db, artist.ID)
	assert.Equal(t, total, totalAfter, "an empty rescan must not lower total_releases")
	assert.Equal(t, acquired, acquiredAfter, "an empty rescan must not lower acquired_releases")
}

func TestRefreshArtistReleaseCounters_AcquiredSurvivesLaterRescan(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	artist := seedArtist(t, db)
	addReleases(t, db, artist.ID, "wanted", "wanted")

	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))
	total, acquired := countersOf(t, db, artist.ID)
	require.Equal(t, 2, total)
	require.Equal(t, 0, acquired)

	// An acquisition finishes and flips both releases to acquired.
	require.NoError(t, db.Model(&database.TrackedRelease{}).
		Where("artist_id = ?", artist.ID).
		Update("status", "acquired").Error)

	// The worker refreshes here, at the moment the truth changed.
	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))

	total, acquired = countersOf(t, db, artist.ID)
	assert.Equal(t, 2, total, "rescanning must not disturb the total")
	assert.Equal(t, 2, acquired, "the acquired count reflects the status change immediately")
}

func TestRefreshArtistReleaseCounters_NoReleasesWritesZero(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	artist := seedArtist(t, db)
	addReleases(t, db, artist.ID, "acquired")
	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))
	require.Equal(t, 1, mustCounters(t, db, artist.ID))

	// An artist whose releases are all gone must be able to return to 0/0.
	// GORM skips zero values in struct updates, so this only works because the
	// write uses a map; without it the row would keep reading 1 forever.
	require.NoError(t, db.Where("artist_id = ?", artist.ID).Delete(&database.TrackedRelease{}).Error)
	require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))

	total, acquired := countersOf(t, db, artist.ID)
	assert.Equal(t, 0, total, "zero must actually be written, not skipped as a struct's zero value")
	assert.Equal(t, 0, acquired)
}

func TestRefreshArtistReleaseCounters_IsIdempotent(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	artist := seedArtist(t, db)
	addReleases(t, db, artist.ID, "acquired", "wanted")

	for i := 0; i < 3; i++ {
		require.NoError(t, RefreshArtistReleaseCounters(db, artist.ID))
	}

	total, acquired := countersOf(t, db, artist.ID)
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, acquired)
}

func TestRefreshArtistReleaseCounters_UnknownArtistIsNotAnError(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	// A scan whose artist was deleted mid-flight should not fail the job.
	assert.NoError(t, RefreshArtistReleaseCounters(db, uuid.New()))
}

func mustCounters(t *testing.T, db *gorm.DB, artistID uuid.UUID) int {
	t.Helper()
	total, _ := countersOf(t, db, artistID)
	return total
}
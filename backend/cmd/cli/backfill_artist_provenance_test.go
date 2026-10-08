package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/agent"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupBackfillDB uses a FILE-backed DSN under t.TempDir(), not ":memory:".
//
// The backfill loops artists through a pooled connection, and a ":memory:"
// SQLite database belongs to the connection that opened it -- the second pooled
// connection sees an empty database and the query dies with "no such table".
// The existing setupTestDB uses ":memory:" and is fine for its single-query
// tests; reusing it here would produce a failure that looks like a bug in the
// backfill rather than a fixture that cannot be pooled.
func setupBackfillDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backfill.db")
	db, err := database.Connect(&config.Config{DatabaseURL: path})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

type backfillArtist struct {
	mbid string
}

func seedBackfillArtist(t *testing.T, db *gorm.DB, mbid string, country, artistType string) database.MonitoredArtist {
	t.Helper()
	profile := database.QualityProfile{Name: "backfill-profile-" + mbid}
	require.NoError(t, db.Create(&profile).Error)
	a := database.MonitoredArtist{
		MusicBrainzID: mbid, Name: "Artist " + mbid,
		Country: country, ArtistType: artistType,
		QualityProfileID: profile.ID,
	}
	require.NoError(t, db.Create(&a).Error)
	return a
}

func backfillGet(known map[string]services.MusicBrainzArtist) func(string) (*services.MusicBrainzArtist, error) {
	return func(id string) (*services.MusicBrainzArtist, error) {
		if a, ok := known[id]; ok {
			return &a, nil
		}
		return nil, services.ErrArtistNotFound
	}
}

// A resolved row gets both fields from the MusicBrainz response.
func TestBackfillArtistProvenance_FillsFromMusicBrainz(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-1", "", "")

	filled, unresolved, complete, err := agent.BackfillArtistProvenance(
		context.Background(), db,
		backfillGet(map[string]services.MusicBrainzArtist{
			"mbid-1": {ID: "mbid-1", Name: "Napalm Death", Country: "United Kingdom", Type: "Group"},
		}), false,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, filled)
	assert.Zero(t, unresolved)
	assert.Zero(t, complete)

	var after database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "mbid-1").First(&after).Error)
	assert.Equal(t, "United Kingdom", after.Country)
	assert.Equal(t, "Group", after.ArtistType)
}

// --dry-run reports what it would fix and writes nothing.
func TestBackfillArtistProvenance_DryRunWritesNothing(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-dry", "", "")

	filled, _, _, err := agent.BackfillArtistProvenance(
		context.Background(), db,
		backfillGet(map[string]services.MusicBrainzArtist{
			"mbid-dry": {ID: "mbid-dry", Name: "Napalm Death", Country: "United Kingdom", Type: "Group"},
		}), true,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, filled, "dry run still counts what it would do")

	var after database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "mbid-dry").First(&after).Error)
	assert.Empty(t, after.Country, "a dry run must not write")
	assert.Empty(t, after.ArtistType, "a dry run must not write")
}

// A row whose MBID no longer resolves is COUNTED, not silently skipped, and the
// caller is expected to fail on a non-zero count so a cron notices.
func TestBackfillArtistProvenance_ReportsUnresolvableRows(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-live", "", "")
	seedBackfillArtist(t, db, "mbid-gone", "", "")

	filled, unresolved, _, err := agent.BackfillArtistProvenance(
		context.Background(), db,
		backfillGet(map[string]services.MusicBrainzArtist{
			"mbid-live": {ID: "mbid-live", Name: "Live", Country: "Sweden", Type: "Group"},
		}), false,
	)
	require.NoError(t, err)

	assert.Equal(t, 1, filled)
	assert.Equal(t, 1, unresolved, "a deleted entity must be reported, not skipped")

	var gone database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "mbid-gone").First(&gone).Error)
	assert.Empty(t, gone.Country, "an unresolvable row is left blank, never invented")
	assert.Empty(t, gone.ArtistType)
}

// A row that already has provenance is counted as complete and not re-fetched —
// re-fetching every row on every run is how a backfill turns into a rate-limit
// problem.
func TestBackfillArtistProvenance_SkipsRowsThatAreAlreadyComplete(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-done", "United Kingdom", "Group")

	called := 0
	get := func(id string) (*services.MusicBrainzArtist, error) {
		called++
		return &services.MusicBrainzArtist{ID: id, Country: "Nowhere", Type: "Solo"}, nil
	}

	filled, unresolved, complete, err := agent.BackfillArtistProvenance(
		context.Background(), db, get, false,
	)
	require.NoError(t, err)

	assert.Zero(t, called, "a complete row must not cost an API call")
	assert.Zero(t, filled)
	assert.Zero(t, unresolved)
	assert.Equal(t, 1, complete)
}

// A partial row (country set, type missing) is still incomplete: MusicBrainz
// entities routinely have one and not the other.
func TestBackfillArtistProvenance_TreatsAPartialRowAsIncomplete(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-partial", "United Kingdom", "")

	filled, _, _, err := agent.BackfillArtistProvenance(
		context.Background(), db,
		backfillGet(map[string]services.MusicBrainzArtist{
			"mbid-partial": {ID: "mbid-partial", Country: "United Kingdom", Type: "Group"},
		}), false,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, filled, "one field missing still means the row needs fetching")

	var after database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "mbid-partial").First(&after).Error)
	assert.Equal(t, "Group", after.ArtistType)
}

// A lookup that fails for a reason other than "not found" — an outage — must be
// reported as unresolved rather than mistaken for a deleted entity, so the
// operator retries instead of concluding the row is gone.
func TestBackfillArtistProvenance_DistinguishesOutageFromNotFound(t *testing.T) {
	db := setupBackfillDB(t)
	seedBackfillArtist(t, db, "mbid-outage", "", "")

	_, unresolved, _, err := agent.BackfillArtistProvenance(
		context.Background(), db,
		func(string) (*services.MusicBrainzArtist, error) {
			return nil, errors.New("dial tcp: i/o timeout")
		}, false,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, unresolved, "an outage must not read as 'that row is gone'")
}

// Nothing to do is not an error, and says so.
func TestBackfillArtistProvenance_EmptyQueueIsNotAnError(t *testing.T) {
	db := setupBackfillDB(t)

	filled, unresolved, complete, err := agent.BackfillArtistProvenance(
		context.Background(), db, backfillGet(nil), false,
	)
	require.NoError(t, err)
	assert.Zero(t, filled)
	assert.Zero(t, unresolved)
	assert.Zero(t, complete)
}

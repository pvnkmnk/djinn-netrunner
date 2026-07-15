package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupSQLiteTestDB creates an in-memory SQLite database for testing.
// It opens a connection, runs AutoMigrate on the provided models, and
// creates any prerequisite records passed as deps.
// Returns the *gorm.DB and fails the test if any setup step errors.
func setupSQLiteTestDB(t *testing.T, models []interface{}, deps ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open in-memory SQLite")
	for _, model := range models {
		require.NoError(t, db.AutoMigrate(model), "failed to AutoMigrate %T", model)
	}
	for _, dep := range deps {
		require.NoError(t, db.Create(dep).Error, "failed to create prerequisite %T", dep)
	}
	return db
}

func TestQualityProfile_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&QualityProfile{}})

	profile := QualityProfile{
		Name:      "Test Profile",
		IsDefault: true,
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, profile.ID)

	err := db.Create(&profile).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, profile.ID)
}

func TestMonitoredArtist_BeforeCreate(t *testing.T) {
	profile := QualityProfile{Name: "Test"}
	db := setupSQLiteTestDB(t, []interface{}{&MonitoredArtist{}, &QualityProfile{}}, &profile)

	artist := MonitoredArtist{
		MusicBrainzID:    "mbid-123",
		Name:             "Test Artist",
		QualityProfileID: profile.ID,
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, artist.ID)

	err := db.Create(&artist).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, artist.ID)
}

func TestTrackedRelease_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&TrackedRelease{}})

	release := TrackedRelease{
		ArtistID:      uuid.New(),
		ReleaseGroupID: "rg-123",
		ReleaseType:   "album",
		Title:         "Test Release",
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, release.ID)

	err := db.Create(&release).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, release.ID)
}

func TestLibrary_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&Library{}})

	library := Library{
		Name: "My Library",
		Path: "/music",
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, library.ID)

	err := db.Create(&library).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, library.ID)
}

func TestTrack_BeforeCreate(t *testing.T) {
	library := Library{Name: "Test Lib", Path: "/test"}
	db := setupSQLiteTestDB(t, []interface{}{&Track{}, &Library{}}, &library)

	track := Track{
		LibraryID: library.ID,
		Title:    "Test Track",
		Artist:   "Test Artist",
		Path:     "/test/track.flac",
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, track.ID)

	err := db.Create(&track).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, track.ID)
}

func TestPlaylist_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&Playlist{}})

	playlist := Playlist{
		Name:        "My Playlist",
		Description: "A test playlist",
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, playlist.ID)

	err := db.Create(&playlist).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, playlist.ID)
}

func TestWatchlist_BeforeCreate(t *testing.T) {
	profile := QualityProfile{Name: "Test"}
	db := setupSQLiteTestDB(t, []interface{}{&Watchlist{}, &QualityProfile{}}, &profile)

	watchlist := Watchlist{
		Name:             "Test Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:123",
		QualityProfileID: profile.ID,
	}
	// ID is nil before create
	assert.Equal(t, uuid.Nil, watchlist.ID)

	err := db.Create(&watchlist).Error
	require.NoError(t, err)

	// ID should be set after create
	assert.NotEqual(t, uuid.Nil, watchlist.ID)
}

func TestJob_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&Job{}})

	job := Job{
		Type:      "sync",
		State:     "queued",
		ScopeType: "watchlist",
		ScopeID:   uuid.New().String(),
	}
	// RequestedAt is zero before create
	assert.True(t, job.RequestedAt.IsZero())

	err := db.Create(&job).Error
	require.NoError(t, err)

	// RequestedAt should be set after create
	assert.False(t, job.RequestedAt.IsZero())
}

func TestJobLog_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&JobLog{}})

	log := JobLog{
		JobID:   1,
		Level:   "INFO",
		Message: "Test log",
	}
	// CreatedAt is zero before create
	assert.True(t, log.CreatedAt.IsZero())

	err := db.Create(&log).Error
	require.NoError(t, err)

	// CreatedAt should be set after create
	assert.False(t, log.CreatedAt.IsZero())
}

func TestAcquisition_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&Acquisition{}})

	acq := Acquisition{
		JobID:      1,
		JobItemID:  1,
		Artist:     "Test Artist",
		TrackTitle: "Test Track",
	}
	// Both times are zero before create
	assert.True(t, acq.AcquiredAt.IsZero())
	assert.True(t, acq.ImportedAt.IsZero())

	err := db.Create(&acq).Error
	require.NoError(t, err)

	// Both times should be set after create
	assert.False(t, acq.AcquiredAt.IsZero())
	assert.False(t, acq.ImportedAt.IsZero())
}

func TestMetadataCache_BeforeCreate(t *testing.T) {
	db := setupSQLiteTestDB(t, []interface{}{&MetadataCache{}})

	cache := MetadataCache{
		Source: "musicbrainz",
		Key:    "artist-123",
		Value:  []byte(`{"name": "Test Artist"}`),
	}
	// CreatedAt is zero before create
	assert.True(t, cache.CreatedAt.IsZero())

	err := db.Create(&cache).Error
	require.NoError(t, err)

	// CreatedAt should be set after create
	assert.False(t, cache.CreatedAt.IsZero())
}

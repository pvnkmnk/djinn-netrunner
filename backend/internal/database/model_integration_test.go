package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpotifyToken_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	user := User{Email: "spotify@test.com", PasswordHash: "hash", Role: "user"}
	err := db.Create(&user).Error
	require.NoError(t, err)

	token := SpotifyToken{
		UserID:       user.ID,
		AccessToken:  "access_token_123",
		RefreshToken: "refresh_token_456",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
		SpDcCookie:   "sp_dc_cookie_value",
	}
	err = db.Create(&token).Error
	require.NoError(t, err)
	assert.NotZero(t, token.ID)

	var found SpotifyToken
	err = db.Where("user_id = ?", user.ID).First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "access_token_123", found.AccessToken)
	assert.Equal(t, "sp_dc_cookie_value", found.SpDcCookie)
}

func TestAcquisition_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	job := Job{Type: "acquisition", State: "succeeded", ScopeType: "artist", ScopeID: uuid.New().String()}
	err := db.Create(&job).Error
	require.NoError(t, err)

	item := JobItem{
		JobID:           job.ID,
		Sequence:        0,
		NormalizedQuery: "artist - track",
		Status:          "succeeded",
	}
	err = db.Create(&item).Error
	require.NoError(t, err)

	acq := Acquisition{
		JobID:      job.ID,
		JobItemID:  item.ID,
		Artist:     "Test Artist",
		TrackTitle: "Test Track",
		FinalPath:  "/music/Test Artist/Test Track.flac",
		FileSize:   25000000,
		FileHash:   "abc123hash",
	}
	err = db.Create(&acq).Error
	require.NoError(t, err)
	assert.NotZero(t, acq.ID)

	var found Acquisition
	err = db.Where("artist = ?", "Test Artist").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "Test Track", found.TrackTitle)
	assert.Equal(t, int64(25000000), found.FileSize)
}

func TestAuditLog_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	log := AuditLog{
		Action:     "user.login",
		ActorID:    1,
		TargetType: "user",
		TargetID:   "1",
		Metadata:   `{"ip": "127.0.0.1"}`,
	}
	err := db.Create(&log).Error
	require.NoError(t, err)
	assert.NotZero(t, log.ID)

	var found AuditLog
	err = db.Where("action = ?", "user.login").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, uint64(1), found.ActorID)
}

func TestPeerReputation_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	peer := PeerReputation{
		Username:       "testpeer",
		TotalDownloads: 100,
		SuccessfulDls:  85,
		FailedDls:      15,
		AvgSpeed:       1024000,
		LastSeen:       time.Now(),
	}
	err := db.Create(&peer).Error
	require.NoError(t, err)

	var found PeerReputation
	err = db.Where("username = ?", "testpeer").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, 100, found.TotalDownloads)
	assert.InDelta(t, 0.85, found.SuccessRate(), 0.001)
	assert.False(t, found.IsIgnored())
}

func TestMetadataCache_CreateAndExpire(t *testing.T) {
	db := setupTestDB(t)

	cache := MetadataCache{
		Source:    "musicbrainz",
		Key:       "artist-mbid-123",
		Value:     []byte(`{"name": "Test Artist", "mbid": "123"}`),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	err := db.Create(&cache).Error
	require.NoError(t, err)
	assert.NotZero(t, cache.ID)

	// Query by source and key
	var found MetadataCache
	err = db.Where("source = ? AND key = ?", "musicbrainz", "artist-mbid-123").First(&found).Error
	require.NoError(t, err)
	assert.Contains(t, string(found.Value), "Test Artist")
}

func TestSetting_Upsert(t *testing.T) {
	db := setupTestDB(t)

	setting := Setting{
		Key:   "app.theme",
		Value: "dark",
		Type:  "string",
	}
	err := db.Create(&setting).Error
	require.NoError(t, err)

	// Update the setting
	err = db.Save(&Setting{
		Key:   "app.theme",
		Value: "light",
		Type:  "string",
	}).Error
	require.NoError(t, err)

	var found Setting
	err = db.Where("key = ?", "app.theme").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "light", found.Value)
}

func TestPlaylist_WithTracks(t *testing.T) {
	db := setupTestDB(t)

	// Create library and tracks
	lib := Library{Name: "Playlist Lib", Path: "/playlist-lib"}
	err := db.Create(&lib).Error
	require.NoError(t, err)

	track1 := Track{LibraryID: lib.ID, Title: "Track 1", Artist: "Artist", Path: "/playlist-lib/track1.flac"}
	track2 := Track{LibraryID: lib.ID, Title: "Track 2", Artist: "Artist", Path: "/playlist-lib/track2.flac"}
	err = db.Create(&track1).Error
	require.NoError(t, err)
	err = db.Create(&track2).Error
	require.NoError(t, err)

	// Create playlist with tracks
	playlist := Playlist{Name: "My Playlist", Description: "Test playlist"}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	// Add tracks to playlist using playlist_tracks join table
	pt1 := PlaylistTrack{PlaylistID: playlist.ID, TrackID: track1.ID, Position: 0}
	pt2 := PlaylistTrack{PlaylistID: playlist.ID, TrackID: track2.ID, Position: 1}
	err = db.Create(&pt1).Error
	require.NoError(t, err)
	err = db.Create(&pt2).Error
	require.NoError(t, err)

	// Query playlist with tracks
	var found Playlist
	err = db.Preload("Tracks").First(&found, playlist.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "My Playlist", found.Name)
	assert.Len(t, found.Tracks, 2)
}

func TestJobLog_WithJob(t *testing.T) {
	db := setupTestDB(t)

	job := Job{Type: "sync", State: "running", ScopeType: "watchlist", ScopeID: uuid.New().String()}
	err := db.Create(&job).Error
	require.NoError(t, err)

	// Create multiple log entries
	log1 := JobLog{JobID: job.ID, Level: "INFO", Message: "Starting sync"}
	log2 := JobLog{JobID: job.ID, Level: "INFO", Message: "Processing tracks"}
	log3 := JobLog{JobID: job.ID, Level: "WARN", Message: "Some tracks skipped"}
	err = db.Create(&log1).Error
	require.NoError(t, err)
	err = db.Create(&log2).Error
	require.NoError(t, err)
	err = db.Create(&log3).Error
	require.NoError(t, err)

	// Query logs for job
	var logs []JobLog
	err = db.Where("job_id = ?", job.ID).Order("created_at ASC").Find(&logs).Error
	require.NoError(t, err)
	assert.Len(t, logs, 3)
	assert.Equal(t, "INFO", logs[0].Level)
	assert.Equal(t, "WARN", logs[2].Level)
}

func TestSchedule_WithWatchlist(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{Name: "Schedule Profile", IsDefault: true}
	err := db.Create(&profile).Error
	require.NoError(t, err)

	watchlist := Watchlist{
		Name:             "Scheduled Watchlist",
		SourceType:       "spotify_playlist",
		SourceURI:        "spotify:playlist:abc",
		QualityProfileID: profile.ID,
		Enabled:          true,
	}
	err = db.Create(&watchlist).Error
	require.NoError(t, err)

	schedule := Schedule{
		WatchlistID: watchlist.ID,
		CronExpr:    "0 0 * * *",
		Timezone:    "America/New_York",
		Enabled:     true,
	}
	err = db.Create(&schedule).Error
	require.NoError(t, err)
	assert.NotZero(t, schedule.ID)
}

func TestQualityProfile_WithAdvancedFilters(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{
		Name:                 "Advanced Profile",
		PreferLossless:       true,
		AllowedFormats:        "flac,wav",
		MinBitrate:           0,
		PreferBitrate:        nil,
		PreferSceneReleases:  true,
		PreferWebReleases:    false,
		MinSampleRate:        48000,
		MinBitDepth:          24,
		FormatPreferenceOrder: JSONStringArray{"flac", "wav", "alac"},
		FilterMode:           FilterModePreferred,
		MaxPeerQueueDepth:     100,
		TranscodeTarget:      "opus",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, profile.ID)

	// Verify advanced fields
	var found QualityProfile
	err = db.First(&found, profile.ID).Error
	require.NoError(t, err)
	assert.Equal(t, 48000, found.MinSampleRate)
	assert.Equal(t, 24, found.MinBitDepth)
	assert.Equal(t, FilterModePreferred, found.FilterMode)
	assert.Equal(t, 100, found.MaxPeerQueueDepth)
	assert.Equal(t, "opus", found.TranscodeTarget)
}

func TestTrackedRelease_WithStatus(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{Name: "Release Profile", IsDefault: true}
	err := db.Create(&profile).Error
	require.NoError(t, err)

	artist := MonitoredArtist{
		MusicBrainzID:    "artist-mbid",
		Name:             "Test Artist",
		QualityProfileID: profile.ID,
	}
	err = db.Create(&artist).Error
	require.NoError(t, err)

	release := TrackedRelease{
		ArtistID:      artist.ID,
		ReleaseGroupID: "rg-mbid",
		ReleaseID:     "release-mbid",
		Title:         "Test Album",
		ReleaseType:   "album",
		Status:        "wanted",
		Monitored:     true,
	}
	err = db.Create(&release).Error
	require.NoError(t, err)

	// Update status to acquired
	now := time.Now()
	err = db.Model(&release).Updates(map[string]interface{}{
		"status":         "acquired",
		"acquired_date":  now,
		"file_path":      "/music/Test Artist/Test Album/01.flac",
		"acquired_format": "FLAC",
	}).Error
	require.NoError(t, err)

	var found TrackedRelease
	err = db.First(&found, release.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "acquired", found.Status)
	assert.NotNil(t, found.AcquiredDate)
	assert.Equal(t, "FLAC", found.AcquiredFormat)
}

func TestMonitoredArtist_ReleaseCounts(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{Name: "Counts Profile", IsDefault: true}
	err := db.Create(&profile).Error
	require.NoError(t, err)

	artist := MonitoredArtist{
		MusicBrainzID:     "mbid-counts",
		Name:              "Artist With Releases",
		QualityProfileID:  profile.ID,
		TotalReleases:     10,
		AcquiredReleases: 3,
	}
	err = db.Create(&artist).Error
	require.NoError(t, err)

	var found MonitoredArtist
	err = db.First(&found, artist.ID).Error
	require.NoError(t, err)
	assert.Equal(t, 10, found.TotalReleases)
	assert.Equal(t, 3, found.AcquiredReleases)
}

package agent

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestProbeSystem(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	cfg := &config.Config{
		GonicURL: "http://localhost:14747",
	}

	status, err := ProbeSystem(db, cfg)
	assert.NoError(t, err)
	assert.True(t, status.DatabaseConnected)
	// We expect Gonic to fail in this test environment
	assert.False(t, status.GonicConnected)
}

func TestConfigTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Setting{})

	cfg := &config.Config{
		Port: "8080",
	}

	// Test ReadConfig
	settings, err := ReadConfig(db, cfg)
	assert.NoError(t, err)
	assert.Equal(t, "8080", settings["port"])

	// Test UpdateConfig
	err = UpdateConfig(db, "custom_setting", "custom_value")
	assert.NoError(t, err)

	// Verify update
	var setting database.Setting
	err = db.First(&setting, "key = ?", "custom_setting").Error
	assert.NoError(t, err)
	assert.Equal(t, "custom_value", setting.Value)
}

func TestWatchlistTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Watchlist{}, &database.QualityProfile{}, &database.User{})

	// Create a default profile and user
	profile := database.QualityProfile{Name: "Standard"}
	db.Create(&profile)
	user := database.User{Email: "agent@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	service := services.NewWatchlistService(db, nil, &config.Config{})

	// Test AddWatchlist
	wl, err := AddWatchlist(service, "Test Watchlist", "lastfm_loved", "testuser", profile.ID, &user.ID)
	assert.NoError(t, err)
	assert.Equal(t, "Test Watchlist", wl.Name)

	// Test ListWatchlists
	lists, err := ListWatchlists(service)
	assert.NoError(t, err)
	assert.Len(t, lists, 1)
}

func TestLibraryTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Library{}, &database.Track{}, &database.Job{}, &database.User{})

	user := database.User{Email: "library@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	// Test AddLibrary
	library, err := AddLibrary(db, "Test Library", "/music/test")
	assert.NoError(t, err)
	assert.Equal(t, "Test Library", library.Name)
	assert.Equal(t, "/music/test", library.Path)

	// Test ListLibraries
	libraries, err := ListLibraries(db)
	assert.NoError(t, err)
	assert.Len(t, libraries, 1)

	// Test ScanLibrary - should create a job
	job, err := ScanLibrary(db, library.ID)
	assert.NoError(t, err)
	assert.Equal(t, "scan", job.Type)
	assert.Equal(t, "queued", job.State)

	// Test DeleteLibrary
	err = DeleteLibrary(db, library.ID)
	assert.NoError(t, err)

	// Verify library is deleted
	libraries, err = ListLibraries(db)
	assert.NoError(t, err)
	assert.Len(t, libraries, 0)
}

func TestJobMonitoringTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.JobLog{}, &database.User{}, &database.JobItem{})

	user := database.User{Email: "job@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	// Create a test job and log
	job := database.Job{Type: "acquisition", State: "running"}
	db.Create(&job)
	db.Create(&database.JobLog{JobID: job.ID, Level: "info", Message: "Starting test job"})

	// Test ListJobs
	jobs, err := ListJobs(db, 10)
	assert.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "running", jobs[0].State)

	// Test GetJobLogs
	logs, err := GetJobLogs(db, job.ID)
	assert.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "Starting test job", logs[0].Message)

	// Test EnqueueAcquisition
	newJob, err := EnqueueAcquisition(db, "New Artist", "New Album", "New Track", &user.ID)
	assert.NoError(t, err)
	assert.Equal(t, "acquisition", newJob.Type)

	var newItem database.JobItem
	err = db.Where("job_id = ?", newJob.ID).First(&newItem).Error
	assert.NoError(t, err)
	assert.Equal(t, "New Artist", newItem.Artist)
}

func TestBootstrap(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Mock config
	cfg := &config.Config{
		GonicURL: "http://localhost:14747",
	}

	// For now, bootstrap just checks env and returns status
	results, err := Bootstrap(db, cfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestSearchLibrary(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Acquisition{})

	// Create a test acquisition
	db.Create(&database.Acquisition{
		Artist:     "Local Artist",
		TrackTitle: "Local Track",
		FinalPath:  "/path/to/music.mp3",
	})

	// Test SearchLibrary (Local only for now)
	results, err := SearchLibrary(db, nil, "Local")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Local Artist", results[0]["artist"])
}

func TestAgentNotification(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Setting{})

	// Test RegisterWebhook
	err = RegisterWebhook(db, "http://agent-callback.com/webhook")
	assert.NoError(t, err)

	// Verify setting
	var setting database.Setting
	err = db.First(&setting, "key = ?", "agent_notification_webhook").Error
	assert.NoError(t, err)
	assert.Equal(t, "http://agent-callback.com/webhook", setting.Value)
}

func TestListMonitoredArtists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.MonitoredArtist{}, &database.QualityProfile{})

	// Create a profile (required for foreign key)
	profile := database.QualityProfile{Name: "Test Profile"}
	db.Create(&profile)

	t.Run("empty", func(t *testing.T) {
		artists, err := ListMonitoredArtists(db)
		assert.NoError(t, err)
		assert.Empty(t, artists)
	})

	t.Run("with data", func(t *testing.T) {
		artist := database.MonitoredArtist{
			MusicBrainzID:      "test-mbid-123",
			Name:               "Test Artist",
			QualityProfileID:    profile.ID,
		}
		assert.NoError(t, db.Create(&artist).Error)

		artists, err := ListMonitoredArtists(db)
		assert.NoError(t, err)
		assert.Len(t, artists, 1)
		assert.Equal(t, "Test Artist", artists[0].Name)
		assert.Equal(t, "test-mbid-123", artists[0].MusicBrainzID)
	})
}

func TestPruneLibrary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Library{}, &database.Job{})

	t.Run("invalid uuid", func(t *testing.T) {
		_, err := PruneLibrary(db, uuid.Nil)
		assert.Error(t, err)
	})

	t.Run("missing library", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := PruneLibrary(db, nonExistentID)
		assert.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		library := database.Library{Name: "Prune Test", Path: "/music/prune"}
		assert.NoError(t, db.Create(&library).Error)

		job, err := PruneLibrary(db, library.ID)
		assert.NoError(t, err)
		assert.NotNil(t, job)
		assert.Equal(t, "prune", job.Type)
		assert.Equal(t, "queued", job.State)
		assert.Equal(t, "library", job.ScopeType)
		assert.Equal(t, library.ID.String(), job.ScopeID)
	})
}

func TestCancelJob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.JobItem{})

	t.Run("missing job", func(t *testing.T) {
		err := CancelJob(db, 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found or cannot be cancelled")
	})

	t.Run("success cancels queued job", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "queued"}
		assert.NoError(t, db.Create(&job).Error)

		err := CancelJob(db, job.ID)
		assert.NoError(t, err)

		// Verify job is cancelled
		var updated database.Job
		assert.NoError(t, db.First(&updated, job.ID).Error)
		assert.Equal(t, "cancelled", updated.State)
	})

	t.Run("success cancels running job", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "running"}
		assert.NoError(t, db.Create(&job).Error)

		item := database.JobItem{JobID: job.ID, Status: "running"}
		assert.NoError(t, db.Create(&item).Error)

		err := CancelJob(db, job.ID)
		assert.NoError(t, err)

		// Verify job and item are cancelled
		var updatedJob database.Job
		assert.NoError(t, db.First(&updatedJob, job.ID).Error)
		assert.Equal(t, "cancelled", updatedJob.State)

		var updatedItem database.JobItem
		assert.NoError(t, db.First(&updatedItem, item.ID).Error)
		assert.Equal(t, "cancelled", updatedItem.Status)
	})

	t.Run("already failed job cannot be cancelled", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "failed"}
		assert.NoError(t, db.Create(&job).Error)

		err := CancelJob(db, job.ID)
		assert.Error(t, err)
	})
}

func TestRetryJob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.JobItem{})

	t.Run("missing job", func(t *testing.T) {
		err := RetryJob(db, 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("non-failed job", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "succeeded"}
		assert.NoError(t, db.Create(&job).Error)

		err := RetryJob(db, job.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in failed state")
	})

	t.Run("running job cannot be retried", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "running"}
		assert.NoError(t, db.Create(&job).Error)

		err := RetryJob(db, job.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in failed state")
	})

	t.Run("queued job cannot be retried", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "queued"}
		assert.NoError(t, db.Create(&job).Error)

		err := RetryJob(db, job.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in failed state")
	})

	t.Run("success resets failed job", func(t *testing.T) {
		job := database.Job{Type: "acquisition", State: "failed"}
		assert.NoError(t, db.Create(&job).Error)

		item := database.JobItem{
			JobID:   job.ID,
			Status:  "failed",
			Artist:  "Test Artist",
			TrackTitle: "Test Track",
		}
		assert.NoError(t, db.Create(&item).Error)

		err := RetryJob(db, job.ID)
		assert.NoError(t, err)

		// Verify job is reset to queued
		var updatedJob database.Job
		assert.NoError(t, db.First(&updatedJob, job.ID).Error)
		assert.Equal(t, "queued", updatedJob.State)

		// Verify item is reset to queued
		var updatedItem database.JobItem
		assert.NoError(t, db.First(&updatedItem, item.ID).Error)
		assert.Equal(t, "queued", updatedItem.Status)
	})
}

func TestListProfiles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.QualityProfile{})

	t.Run("empty", func(t *testing.T) {
		profiles, err := ListProfiles(db)
		assert.NoError(t, err)
		assert.Empty(t, profiles)
	})

	t.Run("with data", func(t *testing.T) {
		profile1 := database.QualityProfile{Name: "Alpha Profile"}
		profile2 := database.QualityProfile{Name: "Beta Profile"}
		db.Create(&profile1)
		db.Create(&profile2)

		profiles, err := ListProfiles(db)
		assert.NoError(t, err)
		assert.Len(t, profiles, 2)
		// Ordered by name
		assert.Equal(t, "Alpha Profile", profiles[0].Name)
		assert.Equal(t, "Beta Profile", profiles[1].Name)
	})
}

func TestGetProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.QualityProfile{})

	t.Run("invalid uuid", func(t *testing.T) {
		_, err := GetProfile(db, uuid.Nil)
		assert.Error(t, err)
	})

	t.Run("missing profile", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := GetProfile(db, nonExistentID)
		assert.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		profile := database.QualityProfile{
			Name:           "Test Profile",
			Description:    "A test description",
			PreferLossless: true,
			AllowedFormats: "flac,mp3",
			MinBitrate:     320,
		}
		db.Create(&profile)

		result, err := GetProfile(db, profile.ID)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "Test Profile", result.Name)
		assert.Equal(t, "A test description", result.Description)
		assert.True(t, result.PreferLossless)
		assert.Equal(t, "flac,mp3", result.AllowedFormats)
		assert.Equal(t, 320, result.MinBitrate)
	})
}

func TestCreateProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.QualityProfile{})

	t.Run("creates profile", func(t *testing.T) {
		profile, err := CreateProfile(
			db,
			"New Profile",
			"A new profile description",
			true,           // preferLossless
			"flac,alac",    // allowedFormats
			320,            // minBitrate
			nil,            // preferBitrate
			true,           // preferScene
			false,          // preferWeb
		)
		assert.NoError(t, err)
		assert.NotNil(t, profile)
		assert.Equal(t, "New Profile", profile.Name)
		assert.Equal(t, "A new profile description", profile.Description)
		assert.True(t, profile.PreferLossless)
		assert.Equal(t, "flac,alac", profile.AllowedFormats)
		assert.Equal(t, 320, profile.MinBitrate)
		assert.Nil(t, profile.PreferBitrate)
		assert.True(t, profile.PreferSceneReleases)
		assert.False(t, profile.PreferWebReleases)
		assert.True(t, profile.IsDefault)
	})

	t.Run("with preferBitrate", func(t *testing.T) {
		bitrate := 256
		profile, err := CreateProfile(
			db,
			"Bitrate Profile",
			"",
			false,
			"mp3",
			128,
			&bitrate,
			false,
			true,
		)
		assert.NoError(t, err)
		assert.NotNil(t, profile)
		assert.NotNil(t, profile.PreferBitrate)
		assert.Equal(t, 256, *profile.PreferBitrate)
	})

	t.Run("clears other defaults", func(t *testing.T) {
		// First profile should be default
		profile1, err := CreateProfile(db, "Profile 1", "", true, "", 0, nil, false, false)
		assert.NoError(t, err)
		assert.True(t, profile1.IsDefault)

		// Second profile should also be default, first should no longer be
		profile2, err := CreateProfile(db, "Profile 2", "", true, "", 0, nil, false, false)
		assert.NoError(t, err)
		assert.True(t, profile2.IsDefault)

		// Verify profile1 is no longer default
		var updated1 database.QualityProfile
		assert.NoError(t, db.First(&updated1, profile1.ID).Error)
		assert.False(t, updated1.IsDefault)

		// Verify profile2 is still default
		var updated2 database.QualityProfile
		assert.NoError(t, db.First(&updated2, profile2.ID).Error)
		assert.True(t, updated2.IsDefault)
	})
}

func TestDeleteProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.QualityProfile{}, &database.Watchlist{}, &database.MonitoredArtist{})

	t.Run("invalid uuid", func(t *testing.T) {
		// DeleteProfile doesn't check existence, just checks for references
		// and then deletes (silently succeeds if not found)
		err := DeleteProfile(db, uuid.Nil)
		assert.NoError(t, err)
	})

	t.Run("missing profile", func(t *testing.T) {
		nonExistentID := uuid.New()
		// DeleteProfile doesn't return error for non-existent profile
		err := DeleteProfile(db, nonExistentID)
		assert.NoError(t, err)
	})

	t.Run("profile in use by watchlist", func(t *testing.T) {
		profile := database.QualityProfile{Name: "In Use By Watchlist"}
		db.Create(&profile)

		watchlist := database.Watchlist{
			Name:             "Test Watchlist",
			SourceType:       "lastfm_loved",
			SourceURI:        "testuser",
			QualityProfileID: profile.ID,
		}
		db.Create(&watchlist)

		err := DeleteProfile(db, profile.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "in use by watchlists")
	})

	t.Run("profile in use by monitored artist", func(t *testing.T) {
		profile := database.QualityProfile{Name: "In Use By Artist"}
		db.Create(&profile)

		artist := database.MonitoredArtist{
			Name:              "Test Artist",
			MusicBrainzID:     "mbid-123",
			QualityProfileID:  profile.ID,
		}
		db.Create(&artist)

		err := DeleteProfile(db, profile.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "in use by monitored artists")
	})

	t.Run("success", func(t *testing.T) {
		profile := database.QualityProfile{Name: "Deletable Profile"}
		db.Create(&profile)

		err := DeleteProfile(db, profile.ID)
		assert.NoError(t, err)

		// Verify deleted
		var count int64
		db.Model(&database.QualityProfile{}).Where("id = ?", profile.ID).Count(&count)
		assert.Equal(t, int64(0), count)
	})
}

func TestSetDefaultProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.QualityProfile{})

	t.Run("invalid uuid", func(t *testing.T) {
		// SetDefaultProfile doesn't check existence - it just updates
		// So this silently succeeds (0 rows affected)
		err := SetDefaultProfile(db, uuid.Nil)
		assert.NoError(t, err)
	})

	t.Run("missing profile", func(t *testing.T) {
		nonExistentID := uuid.New()
		// SetDefaultProfile doesn't return error for non-existent ID
		// because UPDATE affects 0 rows but doesn't error
		err := SetDefaultProfile(db, nonExistentID)
		assert.NoError(t, err)
	})

	t.Run("clears existing defaults", func(t *testing.T) {
		profile1 := database.QualityProfile{Name: "Default 1", IsDefault: true}
		profile2 := database.QualityProfile{Name: "Default 2", IsDefault: false}
		db.Create(&profile1)
		db.Create(&profile2)

		err := SetDefaultProfile(db, profile2.ID)
		assert.NoError(t, err)

		// Verify profile1 is no longer default
		var updated1 database.QualityProfile
		assert.NoError(t, db.First(&updated1, profile1.ID).Error)
		assert.False(t, updated1.IsDefault)

		// Verify profile2 is now default
		var updated2 database.QualityProfile
		assert.NoError(t, db.First(&updated2, profile2.ID).Error)
		assert.True(t, updated2.IsDefault)
	})

	t.Run("success", func(t *testing.T) {
		profile := database.QualityProfile{Name: "Single Default"}
		db.Create(&profile)

		err := SetDefaultProfile(db, profile.ID)
		assert.NoError(t, err)

		var updated database.QualityProfile
		assert.NoError(t, db.First(&updated, profile.ID).Error)
		assert.True(t, updated.IsDefault)
	})
}

func TestGetLibraryStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Library{}, &database.Track{})

	t.Run("empty library", func(t *testing.T) {
		stats, err := GetLibraryStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(0), stats.TotalTracks)
		assert.Equal(t, int64(0), stats.TotalSize)
		assert.Empty(t, stats.FormatBreakdown)
	})

	t.Run("with tracks", func(t *testing.T) {
		library := database.Library{Name: "Stats Library", Path: "/music/stats"}
		db.Create(&library)

		track1 := database.Track{
			LibraryID: library.ID,
			Title:     "Track 1",
			Path:      "/music/stats/track1.flac",
			Format:    "FLAC",
			FileSize:  1024000,
		}
		track2 := database.Track{
			LibraryID: library.ID,
			Title:     "Track 2",
			Path:      "/music/stats/track2.flac",
			Format:    "FLAC",
			FileSize:  2048000,
		}
		track3 := database.Track{
			LibraryID: library.ID,
			Title:     "Track 3",
			Path:      "/music/stats/track3.mp3",
			Format:    "MP3",
			FileSize:  512000,
		}
		db.Create(&track1)
		db.Create(&track2)
		db.Create(&track3)

		stats, err := GetLibraryStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(3), stats.TotalTracks)
		assert.Equal(t, int64(3584000), stats.TotalSize) // 1024KB + 2048KB + 512KB
		assert.Equal(t, float64(3584000)/(1024*1024), stats.TotalSizeMB)
		assert.Len(t, stats.FormatBreakdown, 2)

		// Format breakdown ordered by count DESC (FLAC has 2, MP3 has 1)
		assert.Equal(t, "FLAC", stats.FormatBreakdown[0].Format)
		assert.Equal(t, int64(2), stats.FormatBreakdown[0].Count)
		assert.Equal(t, "MP3", stats.FormatBreakdown[1].Format)
		assert.Equal(t, int64(1), stats.FormatBreakdown[1].Count)
	})
}

func TestSyncWatchlist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.Watchlist{}, &database.User{})

	user := database.User{Email: "sync@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	watchlist := database.Watchlist{
		Name:       "Test Watchlist",
		SourceType: "lastfm_loved",
		SourceURI:  "testuser",
	}
	db.Create(&watchlist)

	t.Run("success creates sync job", func(t *testing.T) {
		job, err := SyncWatchlist(db, watchlist.ID, &user.ID)
		assert.NoError(t, err)
		assert.NotNil(t, job)
		assert.Equal(t, "sync", job.Type)
		assert.Equal(t, "queued", job.State)
		assert.Equal(t, "watchlist", job.ScopeType)
		assert.Equal(t, watchlist.ID.String(), job.ScopeID)
		assert.Equal(t, "cli", job.CreatedBy)
		assert.Equal(t, &user.ID, job.OwnerUserID)
		assert.False(t, job.RequestedAt.IsZero())
	})

	t.Run("creates job without user", func(t *testing.T) {
		job, err := SyncWatchlist(db, watchlist.ID, nil)
		assert.NoError(t, err)
		assert.NotNil(t, job)
		assert.Nil(t, job.OwnerUserID)
	})
}

func TestListDuplicates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Acquisition{}, &database.Job{}, &database.JobItem{})

	// Create job and job items needed for acquisitions
	job := database.Job{Type: "acquisition", State: "succeeded"}
	db.Create(&job)
	item1 := database.JobItem{JobID: job.ID, Status: "completed"}
	item2 := database.JobItem{JobID: job.ID, Status: "completed"}
	item3 := database.JobItem{JobID: job.ID, Status: "completed"}
	item4 := database.JobItem{JobID: job.ID, Status: "completed"}
	db.Create(&item1)
	db.Create(&item2)
	db.Create(&item3)
	db.Create(&item4)

	t.Run("empty database returns nil", func(t *testing.T) {
		groups, err := ListDuplicates(db)
		assert.NoError(t, err)
		assert.Nil(t, groups)
	})

	t.Run("unique MB IDs returns nil", func(t *testing.T) {
		acq1 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item1.ID,
			Artist:        "Artist 1",
			TrackTitle:    "Track 1",
			OriginalPath:  "/path/1.mp3",
			FinalPath:     "/music/1.mp3",
			MBRecordingID: "unique-id-1",
		}
		acq2 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item2.ID,
			Artist:        "Artist 2",
			TrackTitle:    "Track 2",
			OriginalPath:  "/path/2.mp3",
			FinalPath:     "/music/2.mp3",
			MBRecordingID: "unique-id-2",
		}
		db.Create(&acq1)
		db.Create(&acq2)

		groups, err := ListDuplicates(db)
		assert.NoError(t, err)
		assert.Nil(t, groups)
	})

	t.Run("shared MB IDs returns grouped duplicates", func(t *testing.T) {
		// Clear previous acquisitions for this subtest
		db.Exec("DELETE FROM acquisitions")

		// Create two acquisitions with same MB recording ID
		sharedMBID := "shared-recording-id"
		acq1 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item1.ID,
			Artist:        "Same Artist",
			TrackTitle:    "Same Track",
			OriginalPath:  "/path/flac.m4a",
			FinalPath:     "/music/flac.m4a",
			MBRecordingID: sharedMBID,
		}
		acq2 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item2.ID,
			Artist:        "Same Artist",
			TrackTitle:    "Same Track",
			OriginalPath:  "/path/mp3.mp3",
			FinalPath:     "/music/mp3.mp3",
			MBRecordingID: sharedMBID,
		}
		// Acquisition with unique MB ID (should not appear in results)
		acq3 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item3.ID,
			Artist:        "Other Artist",
			TrackTitle:    "Other Track",
			OriginalPath:  "/path/other.mp3",
			FinalPath:     "/music/other.mp3",
			MBRecordingID: "unique-other-id",
		}
		db.Create(&acq1)
		db.Create(&acq2)
		db.Create(&acq3)

		groups, err := ListDuplicates(db)
		assert.NoError(t, err)
		assert.NotNil(t, groups)
		assert.Len(t, groups, 1)

		group := groups[0]
		assert.Equal(t, sharedMBID, group.MBRecordingID)
		assert.Len(t, group.Acquisitions, 2)

		// Verify both acquisitions are returned
		found := make(map[uint64]bool)
		for _, acq := range group.Acquisitions {
			found[acq.ID] = true
		}
		assert.True(t, found[acq1.ID])
		assert.True(t, found[acq2.ID])
		assert.False(t, found[acq3.ID])
	})

	t.Run("empty MBRecordingID is ignored", func(t *testing.T) {
		// Clear and repopulate with empty/nil MB IDs
		db.Exec("DELETE FROM acquisitions")

		acq1 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item1.ID,
			Artist:        "Artist 1",
			TrackTitle:    "Track 1",
			OriginalPath:  "/path/1.mp3",
			FinalPath:     "/music/1.mp3",
			MBRecordingID: "",
		}
		acq2 := database.Acquisition{
			JobID:         job.ID,
			JobItemID:     item2.ID,
			Artist:        "Artist 2",
			TrackTitle:    "Track 2",
			OriginalPath:  "/path/2.mp3",
			FinalPath:     "/music/2.mp3",
			// MBRecordingID intentionally left as zero value (empty string)
		}
		db.Create(&acq1)
		db.Create(&acq2)

		groups, err := ListDuplicates(db)
		assert.NoError(t, err)
		assert.Nil(t, groups)
	})
}

func TestGetJobStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{})

	t.Run("empty database returns zero stats", func(t *testing.T) {
		stats, err := GetJobStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(0), stats.Total)
		assert.Equal(t, int64(0), stats.Queued)
		assert.Equal(t, int64(0), stats.Running)
		assert.Equal(t, int64(0), stats.Succeeded)
		assert.Equal(t, int64(0), stats.Failed)
		assert.Equal(t, float64(0), stats.SuccessRate)
	})

	t.Run("jobs in various states", func(t *testing.T) {
		// Create jobs with different states
		jobs := []database.Job{
			{Type: "acquisition", State: "queued"},
			{Type: "acquisition", State: "running"},
			{Type: "acquisition", State: "succeeded"},
			{Type: "acquisition", State: "succeeded"},
			{Type: "acquisition", State: "succeeded"},
			{Type: "acquisition", State: "failed"},
		}
		for i := range jobs {
			db.Create(&jobs[i])
		}

		stats, err := GetJobStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(6), stats.Total)
		assert.Equal(t, int64(1), stats.Queued)
		assert.Equal(t, int64(1), stats.Running)
		assert.Equal(t, int64(3), stats.Succeeded)
		assert.Equal(t, int64(1), stats.Failed)

		// Success rate = succeeded / (succeeded + failed) * 100
		// = 3 / (3 + 1) * 100 = 75
		assert.Equal(t, float64(75), stats.SuccessRate)
	})

	t.Run("all jobs failed gives zero success rate", func(t *testing.T) {
		db.Exec("DELETE FROM jobs")

		jobs := []database.Job{
			{Type: "acquisition", State: "failed"},
			{Type: "acquisition", State: "failed"},
		}
		for i := range jobs {
			db.Create(&jobs[i])
		}

		stats, err := GetJobStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(2), stats.Total)
		assert.Equal(t, float64(0), stats.SuccessRate)
	})

	t.Run("no completed jobs gives zero success rate", func(t *testing.T) {
		db.Exec("DELETE FROM jobs")

		jobs := []database.Job{
			{Type: "acquisition", State: "queued"},
			{Type: "acquisition", State: "running"},
		}
		for i := range jobs {
			db.Create(&jobs[i])
		}

		stats, err := GetJobStats(db)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, float64(0), stats.SuccessRate)
	})
}

func TestGetStatsSummary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(
		&database.Job{},
		&database.Library{},
		&database.Track{},
		&database.MonitoredArtist{},
		&database.Watchlist{},
		&database.QualityProfile{},
	)

	t.Run("empty database returns zero stats", func(t *testing.T) {
		summary, err := GetStatsSummary(db)
		assert.NoError(t, err)
		assert.NotNil(t, summary)

		// Job stats
		assert.Equal(t, int64(0), summary.Jobs.Total)
		assert.Equal(t, float64(0), summary.Jobs.SuccessRate)

		// Activity stats
		assert.Equal(t, int64(0), summary.Activity.MonitoredArtists)
		assert.Equal(t, int64(0), summary.Activity.Watchlists)
		assert.Equal(t, int64(0), summary.Activity.Libraries)

		// Library stats
		assert.Equal(t, int64(0), summary.Library.TotalTracks)
		assert.Equal(t, int64(0), summary.Library.TotalSize)
		assert.Equal(t, float64(0), summary.Library.TotalSizeMB)
	})

	t.Run("mixed data returns combined stats", func(t *testing.T) {
		// Create jobs
		jobs := []database.Job{
			{Type: "sync", State: "succeeded"},
			{Type: "scan", State: "succeeded"},
			{Type: "acquisition", State: "failed"},
		}
		for i := range jobs {
			db.Create(&jobs[i])
		}

		// Create quality profile (required for watchlist and monitored artist)
		profile := database.QualityProfile{Name: "Test Profile"}
		db.Create(&profile)

		// Create monitored artist
		artist := database.MonitoredArtist{
			Name:              "Test Artist",
			MusicBrainzID:     "artist-mbid-123",
			QualityProfileID:  profile.ID,
		}
		db.Create(&artist)

		// Create watchlist
		watchlist := database.Watchlist{
			Name:             "Test Watchlist",
			SourceType:       "lastfm_loved",
			SourceURI:        "testuser",
			QualityProfileID: profile.ID,
		}
		db.Create(&watchlist)

		// Create library with tracks
		library := database.Library{Name: "Test Library", Path: "/music/test"}
		db.Create(&library)

		tracks := []database.Track{
			{LibraryID: library.ID, Title: "Track 1", Path: "/music/test/1.flac", Format: "FLAC", FileSize: 1024000},
			{LibraryID: library.ID, Title: "Track 2", Path: "/music/test/2.mp3", Format: "MP3", FileSize: 512000},
		}
		for i := range tracks {
			db.Create(&tracks[i])
		}

		summary, err := GetStatsSummary(db)
		assert.NoError(t, err)
		assert.NotNil(t, summary)

		// Job stats (24h window - all jobs qualify)
		assert.Equal(t, int64(3), summary.Jobs.Total)
		assert.Equal(t, int64(2), summary.Jobs.Succeeded)
		assert.Equal(t, int64(1), summary.Jobs.Failed)
		// Success rate = 2/3 * 100 = 66.66...
		assert.InDelta(t, 66.67, summary.Jobs.SuccessRate, 0.01)

		// Activity stats
		assert.Equal(t, int64(1), summary.Activity.MonitoredArtists)
		assert.Equal(t, int64(1), summary.Activity.Watchlists)
		assert.Equal(t, int64(1), summary.Activity.Libraries)

		// Library stats
		assert.Equal(t, int64(2), summary.Library.TotalTracks)
		assert.Equal(t, int64(1536000), summary.Library.TotalSize) // 1024KB + 512KB
		assert.InDelta(t, 1536000.0/(1024*1024), summary.Library.TotalSizeMB, 0.001)
	})
}

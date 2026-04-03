package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestStatsHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &StatsHandler{})
}

func TestJobStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := JobStats{
		Total:       100,
		Queued:      10,
		Running:     5,
		Succeeded:   75,
		Failed:      10,
		SuccessRate: 88.23,
	}

	assert.Equal(t, int64(100), stats.Total)
	assert.Equal(t, int64(75), stats.Succeeded)
	assert.Equal(t, 88.23, stats.SuccessRate)
}

func TestLibraryStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := LibraryStats{
		TotalTracks: 1000,
		TotalSize:   5000000000,
		TotalSizeMB: 4768.37,
		FormatBreakdown: []FormatCount{
			{Format: "FLAC", Count: 800, TotalSize: 4000000000},
			{Format: "MP3", Count: 200, TotalSize: 1000000000},
		},
	}

	assert.Equal(t, int64(1000), stats.TotalTracks)
	assert.Len(t, stats.FormatBreakdown, 2)
	assert.Equal(t, "FLAC", stats.FormatBreakdown[0].Format)
}

func TestActivityStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := ActivityStats{
		MonitoredArtists: 50,
		Watchlists:       10,
		QualityProfiles:  5,
		Libraries:        3,
		RecentJobs24h:    25,
		RecentJobs7d:     100,
	}

	assert.Equal(t, int64(50), stats.MonitoredArtists)
	assert.Equal(t, int64(3), stats.Libraries)
}

func TestStatsHandler_GetActivityStats_Integration(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Seed data
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a1", Name: "Artist 1"})
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a2", Name: "Artist 2"})
	db.Create(&database.Watchlist{Name: "W1", SourceType: "spotify_playlist", SourceURI: "uri1"})
	db.Create(&database.QualityProfile{Name: "P1"})
	db.Create(&database.Library{Name: "L1", Path: "/tmp/l1"})

	now := time.Now()
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: now.Add(-1 * time.Hour)})
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: now.Add(-48 * time.Hour)})

	handler := NewStatsHandler(db)
	app := fiber.New()
	app.Get("/api/stats/activity", handler.GetActivityStats)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/stats/activity", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stats ActivityStats
	err = json.NewDecoder(resp.Body).Decode(&stats)
	assert.NoError(t, err)

	assert.Equal(t, int64(2), stats.MonitoredArtists)
	assert.Equal(t, int64(1), stats.Watchlists)
	assert.Equal(t, int64(1), stats.QualityProfiles)
	assert.Equal(t, int64(1), stats.Libraries)
	assert.Equal(t, int64(1), stats.RecentJobs24h)
	assert.Equal(t, int64(2), stats.RecentJobs7d)
}

func TestStatsHandler_GetSummary_Integration(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Seed data
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a1", Name: "Artist 1"})
	db.Create(&database.Watchlist{Name: "W1", SourceType: "spotify_playlist", SourceURI: "uri1"})
	db.Create(&database.QualityProfile{Name: "P1"})
	db.Create(&database.Library{Name: "L1", Path: "/tmp/l1"})

	now := time.Now()
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: now.Add(-1 * time.Hour)})
	db.Create(&database.Job{Type: "sync", State: "failed", RequestedAt: now.Add(-2 * time.Hour)})

	handler := NewStatsHandler(db)
	app := fiber.New()
	app.Get("/api/stats/summary", handler.GetSummary)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/stats/summary", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stats SummaryStats
	err = json.NewDecoder(resp.Body).Decode(&stats)
	assert.NoError(t, err)

	// Verify Job stats part (existing logic, consolidated query)
	assert.Equal(t, int64(2), stats.Jobs.Total)
	assert.Equal(t, int64(1), stats.Jobs.Succeeded)
	assert.Equal(t, int64(1), stats.Jobs.Failed)
	assert.Equal(t, 50.0, stats.Jobs.SuccessRate)

	// Verify Activity stats part (newly consolidated query)
	assert.Equal(t, int64(1), stats.Activity.MonitoredArtists)
	assert.Equal(t, int64(1), stats.Activity.Watchlists)
	assert.Equal(t, int64(1), stats.Activity.QualityProfiles)
	assert.Equal(t, int64(1), stats.Activity.Libraries)
	assert.Equal(t, int64(2), stats.Activity.RecentJobs24h)
}

func TestStatsHandler_GetLibraryStats_Integration(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Seed data
	lib1 := database.Library{Name: "Library 1", Path: "/tmp/l1"}
	lib2 := database.Library{Name: "Library 2", Path: "/tmp/l2"}
	db.Create(&lib1)
	db.Create(&lib2)

	// Tracks for lib1
	db.Create(&database.Track{LibraryID: lib1.ID, Format: "FLAC", FileSize: 1000, Path: "/tmp/l1/t1.flac", Title: "T1", Artist: "A1"})
	db.Create(&database.Track{LibraryID: lib1.ID, Format: "FLAC", FileSize: 1500, Path: "/tmp/l1/t2.flac", Title: "T2", Artist: "A1"})
	db.Create(&database.Track{LibraryID: lib1.ID, Format: "FLAC", FileSize: 800, Path: "/tmp/l1/t5.flac", Title: "T5", Artist: "A1"})
	db.Create(&database.Track{LibraryID: lib1.ID, Format: "MP3", FileSize: 500, Path: "/tmp/l1/t3.mp3", Title: "T3", Artist: "A2"})

	// Tracks for lib2
	db.Create(&database.Track{LibraryID: lib2.ID, Format: "MP3", FileSize: 600, Path: "/tmp/l2/t4.mp3", Title: "T4", Artist: "A3"})

	handler := NewStatsHandler(db)
	app := fiber.New()
	app.Get("/api/stats/library", handler.GetLibraryStats)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/stats/library", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stats LibraryStats
	err = json.NewDecoder(resp.Body).Decode(&stats)
	assert.NoError(t, err)

	// Verify global totals
	assert.Equal(t, int64(5), stats.TotalTracks)
	assert.Equal(t, int64(4400), stats.TotalSize)

	// Verify format breakdown
	assert.Len(t, stats.FormatBreakdown, 2)
	// They are ordered by count DESC
	assert.Equal(t, "FLAC", stats.FormatBreakdown[0].Format)
	assert.Equal(t, int64(3), stats.FormatBreakdown[0].Count)
	assert.Equal(t, int64(3300), stats.FormatBreakdown[0].TotalSize)

	assert.Equal(t, "MP3", stats.FormatBreakdown[1].Format)
	assert.Equal(t, int64(2), stats.FormatBreakdown[1].Count)
	assert.Equal(t, int64(1100), stats.FormatBreakdown[1].TotalSize)

	// Verify library breakdown
	assert.Len(t, stats.LibraryBreakdown, 2)

	libMap := make(map[string]LibraryCount)
	for _, l := range stats.LibraryBreakdown {
		libMap[l.LibraryID] = l
	}

	assert.Contains(t, libMap, lib1.ID.String())
	assert.Equal(t, "Library 1", libMap[lib1.ID.String()].LibraryName)
	assert.Equal(t, int64(4), libMap[lib1.ID.String()].TrackCount)
	assert.Equal(t, int64(3800), libMap[lib1.ID.String()].TotalSize)

	assert.Contains(t, libMap, lib2.ID.String())
	assert.Equal(t, "Library 2", libMap[lib2.ID.String()].LibraryName)
	assert.Equal(t, int64(1), libMap[lib2.ID.String()].TrackCount)
	assert.Equal(t, int64(600), libMap[lib2.ID.String()].TotalSize)
}

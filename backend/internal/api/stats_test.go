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
	// Mock auth middleware to inject admin user
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", database.User{Role: "admin"})
		return c.Next()
	})
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
	// Mock auth middleware to inject admin user
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", database.User{Role: "admin"})
		return c.Next()
	})
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

	// Setup users
	user1 := database.User{Email: "user1@example.com", Role: "user"}
	user2 := database.User{Email: "user2@example.com", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Setup libraries and tracks for user1
	lib1 := database.Library{Name: "User1 Lib", Path: "/tmp/u1", OwnerUserID: &user1.ID}
	db.Create(&lib1)
	db.Create(&database.Track{
		LibraryID: lib1.ID,
		Title:     "Track 1",
		Format:    "FLAC",
		FileSize:  1000000,
		Path:      "/tmp/u1/track1.flac",
	})

	// Setup libraries and tracks for user2
	lib2 := database.Library{Name: "User2 Lib", Path: "/tmp/u2", OwnerUserID: &user2.ID}
	db.Create(&lib2)
	db.Create(&database.Track{
		LibraryID: lib2.ID,
		Title:     "Track 2",
		Format:    "MP3",
		FileSize:  500000,
		Path:      "/tmp/u2/track2.mp3",
	})

	handler := NewStatsHandler(db)

	// 1. Test BOLA: User1 should only see their own stats
	app1 := fiber.New()
	app1.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user1)
		return c.Next()
	})
	app1.Get("/api/stats/library", handler.GetLibraryStats)

	resp1, err := app1.Test(httptest.NewRequest("GET", "/api/stats/library", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp1.StatusCode)

	var stats1 LibraryStats
	err = json.NewDecoder(resp1.Body).Decode(&stats1)
	assert.NoError(t, err)

	assert.Equal(t, int64(1), stats1.TotalTracks)
	assert.Equal(t, int64(1000000), stats1.TotalSize)
	assert.Len(t, stats1.FormatBreakdown, 1)
	assert.Equal(t, "FLAC", stats1.FormatBreakdown[0].Format)
	assert.Len(t, stats1.LibraryBreakdown, 1)
	assert.Equal(t, "User1 Lib", stats1.LibraryBreakdown[0].LibraryName)

	// 2. Test Admin: should see everything
	appAdmin := fiber.New()
	appAdmin.Use(func(c *fiber.Ctx) error {
		c.Locals("user", database.User{Role: "admin"})
		return c.Next()
	})
	appAdmin.Get("/api/stats/library", handler.GetLibraryStats)

	respAdmin, err := appAdmin.Test(httptest.NewRequest("GET", "/api/stats/library", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, respAdmin.StatusCode)

	var statsAdmin LibraryStats
	err = json.NewDecoder(respAdmin.Body).Decode(&statsAdmin)
	assert.NoError(t, err)

	assert.Equal(t, int64(2), statsAdmin.TotalTracks)
	assert.Equal(t, int64(1500000), statsAdmin.TotalSize)
	assert.Len(t, statsAdmin.FormatBreakdown, 2)
	assert.Len(t, statsAdmin.LibraryBreakdown, 2)
}

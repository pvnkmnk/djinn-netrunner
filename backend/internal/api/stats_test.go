package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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

	// Seed user
	user := database.User{Email: "admin@example.com", Role: "admin"}
	db.Create(&user)

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
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
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

	// Seed user
	user := database.User{Email: "admin@example.com", Role: "admin"}
	db.Create(&user)

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
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
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
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	database.Migrate(db)

	user := database.User{Email: "admin@example.com", Role: "admin"}
	db.Create(&user)

	lib1 := database.Library{ID: uuid.New(), Name: "Library 1", Path: "/tmp/lib1"}
	db.Create(&lib1)

	db.Create(&database.Track{Path: "/tmp/lib1/t1.mp3", LibraryID: lib1.ID, Format: "mp3", FileSize: 1024 * 1024})
	db.Create(&database.Track{Path: "/tmp/lib1/t2.flac", LibraryID: lib1.ID, Format: "flac", FileSize: 2048 * 1024})

	handler := NewStatsHandler(db)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Get("/api/stats/library", handler.GetLibraryStats)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/stats/library", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stats LibraryStats
	json.NewDecoder(resp.Body).Decode(&stats)

	assert.Equal(t, int64(2), stats.TotalTracks)
	assert.Equal(t, int64(3072*1024), stats.TotalSize)
	assert.Equal(t, 3.0, stats.TotalSizeMB)
	assert.Len(t, stats.FormatBreakdown, 2)
	assert.Len(t, stats.LibraryBreakdown, 1)
	assert.Equal(t, "Library 1", stats.LibraryBreakdown[0].LibraryName)
}

func TestStatsHandler_BOLA_Integration(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	database.Migrate(db)

	user1 := database.User{ID: 1, Email: "user1@example.com", Role: "user"}
	user2 := database.User{ID: 2, Email: "user2@example.com", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Seed data for user 1
	db.Create(&database.MonitoredArtist{MusicBrainzID: "u1a1", Name: "U1 Artist 1", OwnerUserID: &user1.ID})
	lib1 := database.Library{ID: uuid.New(), Name: "U1 Library", Path: "/tmp/u1", OwnerUserID: &user1.ID}
	db.Create(&lib1)
	db.Create(&database.Track{Path: "/tmp/u1/t1.mp3", LibraryID: lib1.ID, Format: "mp3", FileSize: 1024})
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: time.Now(), OwnerUserID: &user1.ID})

	// Seed data for user 2
	db.Create(&database.MonitoredArtist{MusicBrainzID: "u2a1", Name: "U2 Artist 1", OwnerUserID: &user2.ID})

	handler := NewStatsHandler(db)

	// Test GetActivityStats as user 1
	app1 := fiber.New()
	app1.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user1)
		return c.Next()
	})
	app1.Get("/api/stats/activity", handler.GetActivityStats)

	resp, err := app1.Test(httptest.NewRequest("GET", "/api/stats/activity", nil))
	assert.NoError(t, err)
	var activity ActivityStats
	json.NewDecoder(resp.Body).Decode(&activity)

	// Should only see user 1's data
	assert.Equal(t, int64(1), activity.MonitoredArtists)
	assert.Equal(t, int64(1), activity.Libraries)
	assert.Equal(t, int64(1), activity.RecentJobs24h)

	// Test GetLibraryStats as user 2
	app2 := fiber.New()
	app2.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user2)
		return c.Next()
	})
	app2.Get("/api/stats/library", handler.GetLibraryStats)

	resp, err = app2.Test(httptest.NewRequest("GET", "/api/stats/library", nil))
	assert.NoError(t, err)
	var library LibraryStats
	json.NewDecoder(resp.Body).Decode(&library)

	// Should see 0 tracks (none belong to user 2's libraries)
	assert.Equal(t, int64(0), library.TotalTracks)
	assert.Len(t, library.LibraryBreakdown, 0)
}

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

func TestStatsHandler_BOLA(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Create two users
	user1 := database.User{Email: "user1@example.com", Role: "user"}
	db.Create(&user1)
	user2 := database.User{Email: "user2@example.com", Role: "user"}
	db.Create(&user2)

	// Create data for user1
	lib1 := database.Library{Name: "Lib 1", OwnerUserID: &user1.ID, Path: "/tmp/lib1"}
	db.Create(&lib1)
	db.Create(&database.Track{LibraryID: lib1.ID, Title: "Track 1", Artist: "Artist 1", Format: "FLAC", FileSize: 1000, Path: "/tmp/t1.flac"})
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user1.ID, RequestedAt: time.Now()})
	db.Create(&database.MonitoredArtist{Name: "Artist 1", MusicBrainzID: "mb1", OwnerUserID: &user1.ID})

	// Create data for user2
	lib2 := database.Library{Name: "Lib 2", OwnerUserID: &user2.ID, Path: "/tmp/lib2"}
	db.Create(&lib2)
	db.Create(&database.Track{LibraryID: lib2.ID, Title: "Track 2", Artist: "Artist 2", Format: "MP3", FileSize: 500, Path: "/tmp/t2.mp3"})
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user2.ID, RequestedAt: time.Now()})

	handler := NewStatsHandler(db)

	t.Run("GetLibraryStats_User1", func(t *testing.T) {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", user1)
			return c.Next()
		})
		app.Get("/stats", handler.GetLibraryStats)

		resp, err := app.Test(httptest.NewRequest("GET", "/stats", nil))
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		var stats LibraryStats
		err = json.NewDecoder(resp.Body).Decode(&stats)
		assert.NoError(t, err)

		// Should only see user1's track
		assert.Equal(t, int64(1), stats.TotalTracks)
		assert.Equal(t, int64(1000), stats.TotalSize)
		assert.Len(t, stats.FormatBreakdown, 1)
		assert.Equal(t, "FLAC", stats.FormatBreakdown[0].Format)
		assert.Len(t, stats.LibraryBreakdown, 1)
		assert.Equal(t, "Lib 1", stats.LibraryBreakdown[0].LibraryName)
	})

	t.Run("GetSummary_User2", func(t *testing.T) {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", user2)
			return c.Next()
		})
		app.Get("/summary", handler.GetSummary)

		resp, err := app.Test(httptest.NewRequest("GET", "/summary", nil))
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		var summary SummaryStats
		err = json.NewDecoder(resp.Body).Decode(&summary)
		assert.NoError(t, err)

		// Should only see user2's stats
		assert.Equal(t, int64(1), summary.Jobs.Total)
		assert.Equal(t, int64(1), summary.Library.TotalTracks)
		assert.Equal(t, int64(1), summary.Activity.Libraries)
		assert.Equal(t, int64(0), summary.Activity.MonitoredArtists) // User 1 has the artist
	})

	t.Run("GetActivityStats_Admin", func(t *testing.T) {
		admin := database.User{Email: "admin@example.com", Role: "admin"}
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", admin)
			return c.Next()
		})
		app.Get("/activity", handler.GetActivityStats)

		resp, err := app.Test(httptest.NewRequest("GET", "/activity", nil))
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		var stats ActivityStats
		err = json.NewDecoder(resp.Body).Decode(&stats)
		assert.NoError(t, err)

		// Admin should see everything
		assert.Equal(t, int64(2), stats.Libraries)
		assert.Equal(t, int64(2), stats.RecentJobs24h)
	})
}

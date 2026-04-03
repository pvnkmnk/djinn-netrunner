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

func TestStatsBOLA_Repro(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Setup users
	user1 := database.User{Email: "user1@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Seed data for user1
	now := time.Now()
	db.Create(&database.Job{
		Type:        "sync",
		State:       "succeeded",
		RequestedAt: now.Add(-1 * time.Hour),
		OwnerUserID: &user1.ID,
	})

	lib1 := database.Library{Name: "User1 Lib", Path: "/tmp/u1", OwnerUserID: &user1.ID}
	db.Create(&lib1)
	db.Create(&database.Track{
		LibraryID: lib1.ID,
		Title:     "User1 Track",
		Artist:    "User1 Artist",
		Path:      "/tmp/u1/track1.mp3",
		Format:    "MP3",
		FileSize:  1000,
	})

	db.Create(&database.MonitoredArtist{
		MusicBrainzID: "mb1",
		Name:          "User1 Artist",
		OwnerUserID:   &user1.ID,
	})

	handler := NewStatsHandler(db)
	app := fiber.New()

	// Mock Auth Middleware for simplicity in repro
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user2)
		return c.Next()
	})

	// Test endpoints
	app.Get("/api/stats/jobs", handler.GetJobStats)
	app.Get("/api/stats/library", handler.GetLibraryStats)
	app.Get("/api/stats/activity", handler.GetActivityStats)

	// 1. Check Job Stats - User 2 should NOT see User 1's jobs
	req := httptest.NewRequest("GET", "/api/stats/jobs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var jobStats JobStats
	json.NewDecoder(resp.Body).Decode(&jobStats)

	t.Logf("Job Total: %d", jobStats.Total)

	// 2. Check Library Stats - User 2 should NOT see User 1's tracks
	req = httptest.NewRequest("GET", "/api/stats/library", nil)
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var libStats LibraryStats
	json.NewDecoder(resp.Body).Decode(&libStats)

	t.Logf("Track Total: %d", libStats.TotalTracks)

	// 3. Check Activity Stats - User 2 should NOT see User 1's artists/libraries
	req = httptest.NewRequest("GET", "/api/stats/activity", nil)
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var actStats ActivityStats
	json.NewDecoder(resp.Body).Decode(&actStats)

	t.Logf("Monitored Artists: %d", actStats.MonitoredArtists)
	t.Logf("Libraries: %d", actStats.Libraries)

	// Assert the secure state (should be 0).
	// This is expected to FAIL before the fix.
	assert.Equal(t, int64(0), jobStats.Total, "BOLA: User 2 can see User 1's job stats")
	assert.Equal(t, int64(0), libStats.TotalTracks, "BOLA: User 2 can see User 1's library stats")
	assert.Equal(t, int64(0), actStats.MonitoredArtists, "BOLA: User 2 can see User 1's activity stats")
}

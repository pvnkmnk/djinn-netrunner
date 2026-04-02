package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestStatsBOLA(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Setup users
	user1 := database.User{Email: "user1@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Seed data for user 1
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a1", Name: "Artist 1", OwnerUserID: &user1.ID})
	db.Create(&database.Watchlist{Name: "W1", SourceType: "spotify_playlist", SourceURI: "uri1", OwnerUserID: &user1.ID})
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: time.Now(), OwnerUserID: &user1.ID})

	// Seed data for user 2
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a2", Name: "Artist 2", OwnerUserID: &user2.ID})

	handler := NewStatsHandler(db)
	app := fiber.New()

	// Mock middleware to inject user
	app.Use(func(c *fiber.Ctx) error {
		sessionID := c.Cookies(SessionCookie)
		var user database.User
		if sessionID == "sess1" {
			user = user1
		} else if sessionID == "sess2" {
			user = user2
		} else {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}
		c.Locals("user", user)
		return c.Next()
	})

	app.Get("/api/stats/activity", handler.GetActivityStats)

	// 1. User 1 should see their own stats (1 artist, 1 watchlist)
	req := httptest.NewRequest("GET", "/api/stats/activity", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess1"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var stats1 ActivityStats
	json.NewDecoder(resp.Body).Decode(&stats1)
	assert.Equal(t, int64(1), stats1.MonitoredArtists, "User 1 should see only 1 artist")
	assert.Equal(t, int64(1), stats1.Watchlists, "User 1 should see only 1 watchlist")

	// 2. User 2 should see their own stats (1 artist, 0 watchlists)
	req = httptest.NewRequest("GET", "/api/stats/activity", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var stats2 ActivityStats
	json.NewDecoder(resp.Body).Decode(&stats2)
	assert.Equal(t, int64(1), stats2.MonitoredArtists, "User 2 should see only 1 artist")
	assert.Equal(t, int64(0), stats2.Watchlists, "User 2 should see 0 watchlists")
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestStatsAuthorization(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()

	auth := NewAuthHandler(db)
	statsHandler := NewStatsHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/stats/activity", statsHandler.GetActivityStats)
	app.Get("/api/stats/summary", statsHandler.GetSummary)

	// Setup users
	user1 := database.User{Email: "user1@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Setup sessions
	sess1 := database.Session{SessionID: "sess1", UserID: user1.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	sess2 := database.Session{SessionID: "sess2", UserID: user2.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	db.Create(&sess1)
	db.Create(&sess2)

	// Setup data for user1
	db.Create(&database.MonitoredArtist{MusicBrainzID: "a1", Name: "Artist 1", OwnerUserID: &user1.ID})
	db.Create(&database.Watchlist{Name: "W1", SourceType: "spotify_playlist", SourceURI: "uri1", OwnerUserID: &user1.ID})

	// 1. User2 tries to get activity stats - should NOT see User1's data
	req := httptest.NewRequest("GET", "/api/stats/activity", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var activity ActivityStats
	json.NewDecoder(resp.Body).Decode(&activity)

	// If BOLA exists, these will be 1. If fixed, they should be 0.
	assert.Equal(t, int64(0), activity.MonitoredArtists, "User2 should not see User1's monitored artists count")
	assert.Equal(t, int64(0), activity.Watchlists, "User2 should not see User1's watchlists count")

	// 2. User2 tries to get summary stats
	req = httptest.NewRequest("GET", "/api/stats/summary", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var summary SummaryStats
	json.NewDecoder(resp.Body).Decode(&summary)
	assert.Equal(t, int64(0), summary.Activity.MonitoredArtists, "User2 should not see User1's monitored artists count in summary")
}

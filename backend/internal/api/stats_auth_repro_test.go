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

func TestStatsBOLA(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()

	auth := NewAuthHandler(db)
	statsHandler := NewStatsHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/stats/activity", statsHandler.GetActivityStats)
	app.Get("/api/stats/jobs", statsHandler.GetJobStats)
	app.Get("/api/stats/summary", statsHandler.GetSummary)
	app.Get("/api/stats/library", statsHandler.GetLibraryStats)

	// Setup users
	user1 := database.User{Email: "u1@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "u2@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Setup sessions
	sess2 := database.Session{SessionID: "sess-user2", UserID: user2.ID, ExpiresAt: time.Now().Add(24 * time.Hour)}
	db.Create(&sess2)

	// Data for user 1 (the "victim")
	db.Create(&database.MonitoredArtist{Name: "U1 Artist", OwnerUserID: &user1.ID, MusicBrainzID: "u1-artist-mbid"})
	lib1 := database.Library{Name: "U1 Lib", Path: "/u1/lib", OwnerUserID: &user1.ID}
	db.Create(&lib1)
	db.Create(&database.Track{Title: "U1 Track", LibraryID: lib1.ID, Path: "/u1/lib/track1.flac", FileSize: 1024 * 1024})
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user1.ID, RequestedAt: time.Now()})

	// 1. User 2 checks activity stats
	// VULNERABILITY: User 2 should NOT see User 1's monitored artists or libraries
	req := httptest.NewRequest("GET", "/api/stats/activity", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-user2"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var activity ActivityStats
	json.NewDecoder(resp.Body).Decode(&activity)

	// These should be 0 for user 2
	assert.Equal(t, int64(0), activity.MonitoredArtists, "User 2 should NOT see User 1's monitored artists")
	assert.Equal(t, int64(0), activity.Libraries, "User 2 should NOT see User 1's libraries")

	// 2. User 2 checks job stats
	req = httptest.NewRequest("GET", "/api/stats/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-user2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var jobStats JobStats
	json.NewDecoder(resp.Body).Decode(&jobStats)
	assert.Equal(t, int64(0), jobStats.Total, "User 2 should NOT see User 1's jobs")

	// 3. User 2 checks library stats
	req = httptest.NewRequest("GET", "/api/stats/library", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-user2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var libStats LibraryStats
	json.NewDecoder(resp.Body).Decode(&libStats)
	assert.Equal(t, int64(0), libStats.TotalTracks, "User 2 should NOT see User 1's tracks")
}

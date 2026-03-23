package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestArtistsAuthorization(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()

	auth := NewAuthHandler(db)
	mbService := services.NewMusicBrainzService(nil)
	atService := services.NewArtistTrackingService(db, mbService)
	artistsHandler := NewArtistsHandler(db, atService, mbService)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/artists", artistsHandler.List)
	app.Post("/api/artists", artistsHandler.Add)
	app.Delete("/api/artists/:id", artistsHandler.Delete)
	app.Patch("/api/artists/:id", artistsHandler.Update)

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

	// Setup quality profile
	qp := database.QualityProfile{Name: "Test Profile for Artists"}
	db.Create(&qp)

	// Setup monitored artist for user1
	artist1 := database.MonitoredArtist{
		ID:               uuid.New(),
		MusicBrainzID:    "artist-1-mbid",
		Name:             "User1 Artist",
		QualityProfileID: qp.ID,
		OwnerUserID:      &user1.ID,
		Monitored:        true,
	}
	db.Create(&artist1)

	// 1. User2 tries to list artists - should NOT see User1's artist
	req := httptest.NewRequest("GET", "/api/artists", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var artists []database.MonitoredArtist
	json.NewDecoder(resp.Body).Decode(&artists)
	assert.Empty(t, artists, "User2 should not see User1's artists")

	// 2. User2 tries to delete User1's artist - should be 200 (but deleted count 0) or 404/403
	// The current implementation of DeleteMonitoredArtist returns success even if no rows were deleted if GORM didn't error.
	// But it uses .Where("owner_user_id = ?", userID) so it won't delete it.
	req = httptest.NewRequest("DELETE", "/api/artists/"+artist1.ID.String(), nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Verify it still exists
	var checkArtist database.MonitoredArtist
	err := db.First(&checkArtist, "id = ?", artist1.ID).Error
	assert.Nil(t, err)

	// 3. User2 tries to update User1's artist - should be 500 (since it fails to reload it) or 403
	updatePayload := map[string]bool{"monitored": false}
	body, _ := json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/artists/"+artist1.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 500, resp.StatusCode, "User2 should not be able to update User1's artist (reloading fails)")

	db.First(&checkArtist, "id = ?", artist1.ID)
	assert.True(t, checkArtist.Monitored)
}

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
	// Clear tables to ensure fresh test state
	db.Exec("DELETE FROM jobitems")
	db.Exec("DELETE FROM jobs")
	db.Exec("DELETE FROM sessions")
	db.Exec("DELETE FROM users")
	db.Exec("DELETE FROM monitored_artists")
	db.Exec("DELETE FROM quality_profiles")

	app := fiber.New()

	auth := NewAuthHandler(db)
	atService := services.NewArtistTrackingService(db, nil)
	artistsHandler := NewArtistsHandler(db, atService, nil)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/artists", artistsHandler.List)
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

	// 1. User2 tries to list artists - currently they see EVERYTHING because List doesn't filter
	req := httptest.NewRequest("GET", "/api/artists", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var artists []database.MonitoredArtist
	json.NewDecoder(resp.Body).Decode(&artists)

	// THIS IS THE VULNERABILITY: User2 should NOT see User1's artist
	// In a secure system, this should be empty.
	// We expect this to fail once we fix it.
	found := false
	for _, a := range artists {
		if a.ID == artist1.ID {
			found = true
			break
		}
	}

	assert.False(t, found, "User2 should not see User1's artist")

	// 2. User2 tries to delete User1's artist - should be 403
	req = httptest.NewRequest("DELETE", "/api/artists/"+artist1.ID.String(), nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode, "User2 should be forbidden from deleting User1's artist")

	// 3. User2 tries to update User1's artist - should be 403
	updatePayload := map[string]bool{"monitored": false}
	body, _ := json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/artists/"+artist1.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode, "User2 should be forbidden from updating User1's artist")
}

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
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestWatchlistBOLA(t *testing.T) {
	db := setupTestDBForAuth(t)
	// Use Pongo2 engine for HTMX partials
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	auth := NewAuthHandler(db)
	spotifyAuth := NewSpotifyAuthHandler(db)
	cfg := &config.Config{}
	watchlistService := services.NewWatchlistService(db, spotifyAuth, cfg)
	watchlistHandler := NewWatchlistHandler(db, watchlistService)
	watchlistPreviewHandler := NewWatchlistPreviewHandler(db, watchlistService)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/watchlists/:id/preview", watchlistPreviewHandler.GetPreview)
	app.Get("/api/watchlists/form", watchlistHandler.GetForm)
	app.Post("/api/watchlists", watchlistHandler.CreateWatchlist)
	app.Patch("/api/watchlists/:id", watchlistHandler.UpdateWatchlist)

	// Use unique emails with UUID for test isolation
	testID := uuid.New().String()
	user1 := database.User{Email: "user1-" + testID + "@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2-" + testID + "@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Setup sessions
	sess1 := database.Session{SessionID: "sess1-" + testID, UserID: user1.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	sess2 := database.Session{SessionID: "sess2-" + testID, UserID: user2.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	db.Create(&sess1)
	db.Create(&sess2)

	// Cleanup function to remove test data
	defer func() {
		db.Delete(&database.Session{}, "session_id LIKE ?", "%"+testID)
		db.Delete(&database.Watchlist{}, "name LIKE ?", "%"+testID+"%")
		db.Delete(&database.QualityProfile{}, "name LIKE ?", "%"+testID+"%")
		db.Delete(&database.User{}, "email LIKE ?", "%"+testID+"@%")
	}()

	// Setup private quality profile owned by User1
	qpPrivateUser1 := database.QualityProfile{
		Name:        "User1 Private Profile-" + testID,
		OwnerUserID: &user1.ID,
		IsDefault:   false,
	}
	db.Create(&qpPrivateUser1)

	// Setup public/default quality profile
	qp := database.QualityProfile{Name: "Test Profile for Watchlists-" + testID, IsDefault: true}
	db.Create(&qp)

	// Setup watchlist for user1
	wl1 := database.Watchlist{
		ID:               uuid.New(),
		Name:             "User1 Watchlist-" + testID,
		SourceType:       "local_file",
		SourceURI:        "test-" + testID + ".txt",
		QualityProfileID: qp.ID,
		OwnerUserID:      &user1.ID,
		Enabled:          true,
	}
	db.Create(&wl1)

	// 1. User2 tries to preview User1's watchlist - SHOULD FAIL with 403 or 404
	req := httptest.NewRequest("GET", "/api/watchlists/"+wl1.ID.String()+"/preview", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess2.SessionID})
	resp, _ := app.Test(req)

	// Expect failure (403 Forbidden or 404 Not Found)
	assert.Equal(t, 403, resp.StatusCode, "BOLA: User2 should NOT be able to access User1's watchlist preview")

	// 2. User2 tries to get form for User1's watchlist - SHOULD FAIL or return error snippet
	req = httptest.NewRequest("GET", "/api/watchlists/form?id="+wl1.ID.String(), nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess2.SessionID})
	req.Header.Set("Htmx-Request", "true")
	resp, _ = app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	// Check that response contains "Watchlist not found" and NOT the form content
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	assert.Contains(t, body.String(), "Watchlist not found")
	assert.NotContains(t, body.String(), "User1 Watchlist")

	// 3. User2 tries to create a Watchlist using User1's private quality profile - SHOULD FAIL with 403
	createPayload := map[string]interface{}{
		"name":               "User2 Evil Watchlist-" + testID,
		"source_type":        "local_file",
		"source_uri":         "test-user2-" + testID + ".txt",
		"quality_profile_id": qpPrivateUser1.ID.String(),
	}
	createBody, _ := json.Marshal(createPayload)
	req = httptest.NewRequest("POST", "/api/watchlists", bytes.NewBuffer(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess2.SessionID})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode, "BOLA: User2 should NOT be allowed to assign User1's private quality profile on create")

	// 4. User2 creates a Watchlist with default profile, then tries to UPDATE it to User1's private profile - SHOULD FAIL with 403
	wl2 := database.Watchlist{
		ID:               uuid.New(),
		Name:             "User2 Watchlist-" + testID,
		SourceType:       "local_file",
		SourceURI:        "test-user2-valid-" + testID + ".txt",
		QualityProfileID: qp.ID,
		OwnerUserID:      &user2.ID,
		Enabled:          true,
	}
	db.Create(&wl2)

	updatePayload := map[string]interface{}{
		"quality_profile_id": qpPrivateUser1.ID.String(),
	}
	updateBody, _ := json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/watchlists/"+wl2.ID.String(), bytes.NewBuffer(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess2.SessionID})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode, "BOLA: User2 should NOT be allowed to update watchlist to User1's private quality profile")
}

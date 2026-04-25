package api

import (
	"bytes"
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
	db := setupTestDB(t)
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
	qp := database.QualityProfile{Name: "Test Profile for Watchlists"}
	db.Create(&qp)

	// Setup watchlist for user1
	wl1 := database.Watchlist{
		ID:               uuid.New(),
		Name:             "User1 Watchlist",
		SourceType:       "local_file",
		SourceURI:        "test.txt",
		QualityProfileID: qp.ID,
		OwnerUserID:      &user1.ID,
		Enabled:          true,
	}
	db.Create(&wl1)

	// 1. User2 tries to preview User1's watchlist - SHOULD FAIL with 403 or 404
	req := httptest.NewRequest("GET", "/api/watchlists/"+wl1.ID.String()+"/preview", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)

	// Expect failure (403 Forbidden or 404 Not Found)
	assert.Equal(t, 403, resp.StatusCode, "BOLA: User2 should NOT be able to access User1's watchlist preview")

	// 2. User2 tries to get form for User1's watchlist - SHOULD FAIL or return error snippet
	req = httptest.NewRequest("GET", "/api/watchlists/form?id="+wl1.ID.String(), nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	req.Header.Set("Htmx-Request", "true")
	resp, _ = app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	// Check that response contains "Watchlist not found" and NOT the form content
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	assert.Contains(t, body.String(), "Watchlist not found")
	assert.NotContains(t, body.String(), "User1 Watchlist")
}

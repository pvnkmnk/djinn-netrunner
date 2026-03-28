package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestWatchlistBOLA(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New(fiber.Config{
		Views: nil, // We don't need real templates, 500 means it reached Render (vulnerable), 200/403 means it was blocked
	})

	auth := NewAuthHandler(db)
	cfg := &config.Config{}
	watchlistService := services.NewWatchlistService(db, nil, cfg)
	watchlistHandler := NewWatchlistHandler(db, watchlistService)
	previewHandler := NewWatchlistPreviewHandler(db, watchlistService)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/watchlists/form", watchlistHandler.GetForm)
	app.Get("/api/watchlists/:id/preview", previewHandler.GetPreview)

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

	// Setup watchlist for user1
	wl1 := database.Watchlist{
		ID:          uuid.New(),
		Name:        "User1 Watchlist",
		SourceType:  "local_file",
		SourceURI:   "/tmp/test.txt",
		OwnerUserID: &user1.ID,
	}
	db.Create(&wl1)

	// 1. User2 tries to access User1's watchlist form
	req := httptest.NewRequest("GET", "/api/watchlists/form?id="+wl1.ID.String(), nil)
	req.Header.Set("Htmx-Request", "true")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)

	// If it returns 500, it means it reached c.Render (vulnerable)
	// If it returns 200, we check if it says "not found"
	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "Watchlist not found.", "User2 should not be able to access User1's watchlist form")
	} else if resp.StatusCode == 500 {
		t.Errorf("VULNERABLE: User2 reached Render for User1's watchlist form (BOLA)")
	}

	// 2. User2 tries to access User1's watchlist preview
	req = httptest.NewRequest("GET", "/api/watchlists/"+wl1.ID.String()+"/preview", nil)
	req.Header.Set("Htmx-Request", "true")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)

	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "watchlist not found", "User2 should not be able to access User1's watchlist preview")
	} else if resp.StatusCode == 500 {
		t.Errorf("VULNERABLE: User2 reached Render for User1's watchlist preview (BOLA)")
	}
}

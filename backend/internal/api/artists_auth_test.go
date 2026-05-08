package api

import (
	"bytes"
	"encoding/json"
	"io"
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

type captureViews struct {
	lastTemplate string
	lastBinding  fiber.Map
}

func (v *captureViews) Load() error {
	return nil
}

func (v *captureViews) Render(w io.Writer, template string, binding interface{}, _ ...string) error {
	v.lastTemplate = template
	if m, ok := binding.(fiber.Map); ok {
		v.lastBinding = m
	} else {
		v.lastBinding = fiber.Map{}
	}
	_, err := w.Write([]byte("rendered"))
	return err
}

func TestArtistsAuthorization(t *testing.T) {
	db := setupInMemoryDB(t)
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

func TestArtistsGetForm_NonAdminPrefersLocalsAndFiltersProfiles(t *testing.T) {
	db := setupInMemoryDB(t)
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	views := &captureViews{}
	app := fiber.New(fiber.Config{Views: views})

	mbService := services.NewMusicBrainzService(nil)
	atService := services.NewArtistTrackingService(db, mbService)
	artistsHandler := NewArtistsHandler(db, atService, mbService)

	user1 := database.User{Email: "user1-form@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2-form@example.com", PasswordHash: "hash", Role: "user"}
	assert.NoError(t, db.Create(&user1).Error)
	assert.NoError(t, db.Create(&user2).Error)

	sess2 := database.Session{SessionID: "sess-form-user2", UserID: user2.ID, ExpiresAt: time.Now().Add(24 * time.Hour)}
	assert.NoError(t, db.Create(&sess2).Error)

	assert.NoError(t, db.Create(&database.QualityProfile{Name: "User1 Owned Profile", OwnerUserID: &user1.ID}).Error)
	assert.NoError(t, db.Create(&database.QualityProfile{Name: "Shared Profile"}).Error)
	assert.NoError(t, db.Create(&database.QualityProfile{Name: "Default Profile", OwnerUserID: &user2.ID, IsDefault: true}).Error)
	assert.NoError(t, db.Create(&database.QualityProfile{Name: "User2 Private Profile", OwnerUserID: &user2.ID}).Error)

	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user1)
		return c.Next()
	})
	app.Get("/api/artists/form", artistsHandler.GetForm)

	req := httptest.NewRequest("GET", "/api/artists/form", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-form-user2"})
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	assert.Equal(t, "partials/artist-form", views.lastTemplate)
	profiles, ok := views.lastBinding["profiles"].([]database.QualityProfile)
	assert.True(t, ok)
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "User1 Owned Profile")
	assert.Contains(t, names, "Shared Profile")
	assert.Contains(t, names, "Default Profile")
	assert.NotContains(t, names, "User2 Private Profile")
}

func TestArtistsGetForm_AdminSeesAllProfiles(t *testing.T) {
	db := setupInMemoryDB(t)
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	views := &captureViews{}
	app := fiber.New(fiber.Config{Views: views})

	mbService := services.NewMusicBrainzService(nil)
	atService := services.NewArtistTrackingService(db, mbService)
	artistsHandler := NewArtistsHandler(db, atService, mbService)

	admin := database.User{Email: "admin-form@example.com", PasswordHash: "hash", Role: "admin"}
	other := database.User{Email: "other-form@example.com", PasswordHash: "hash", Role: "user"}
	assert.NoError(t, db.Create(&admin).Error)
	assert.NoError(t, db.Create(&other).Error)

	assert.NoError(t, db.Create(&database.QualityProfile{Name: "Admin View Shared"}).Error)
	assert.NoError(t, db.Create(&database.QualityProfile{Name: "Admin View Other Owned", OwnerUserID: &other.ID}).Error)
	assert.NoError(t, db.Create(&database.QualityProfile{Name: "Admin View Default", OwnerUserID: &other.ID, IsDefault: true}).Error)

	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", admin)
		return c.Next()
	})
	app.Get("/api/artists/form", artistsHandler.GetForm)

	req := httptest.NewRequest("GET", "/api/artists/form", nil)
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	assert.Equal(t, "partials/artist-form", views.lastTemplate)
	profiles, ok := views.lastBinding["profiles"].([]database.QualityProfile)
	assert.True(t, ok)
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "Admin View Shared")
	assert.Contains(t, names, "Admin View Other Owned")
	assert.Contains(t, names, "Admin View Default")
}

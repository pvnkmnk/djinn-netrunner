package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtistsHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &ArtistsHandler{})
}

func TestLibraryHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &LibraryHandler{})
}

func TestArtistsHandler_SyncQueuesArtistScanJob(t *testing.T) {
	db := setupAPITestDB(t)
	user := database.User{Email: "artist-sync-api@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "artist-sync-api-profile", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&profile).Error)
	artist := database.MonitoredArtist{
		MusicBrainzID:    "artist-sync-api-mbid",
		Name:             "Artist Sync API",
		QualityProfileID: profile.ID,
		OwnerUserID:      &user.ID,
		Monitored:        true,
	}
	require.NoError(t, db.Create(&artist).Error)

	handler := NewArtistsHandler(db, services.NewArtistTrackingService(db, services.NewMusicBrainzService(nil)), nil)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Post("/api/artists/:id/sync", handler.Sync)

	req := httptest.NewRequest("POST", "/api/artists/"+artist.ID.String()+"/sync", nil)
	resp, err := app.Test(req)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "sync_queued", body["status"])

	var job database.Job
	require.NoError(t, db.First(&job, "job_type = ? AND scope_id = ?", "artist_scan", artist.ID.String()).Error)
	assert.Equal(t, "queued", job.State)
	assert.Equal(t, "artist", job.ScopeType)
	assert.Equal(t, &user.ID, job.OwnerUserID)
	assert.Equal(t, "user_api", job.CreatedBy)
}

func TestArtistsHandler_RenderPartialHandlesNeverScannedArtist(t *testing.T) {
	db := setupAPITestDB(t)
	user := database.User{Email: "artist-partial@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "artist-partial-profile", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&profile).Error)
	artist := database.MonitoredArtist{
		MusicBrainzID:    "artist-partial-mbid",
		Name:             "Never Scanned Artist",
		QualityProfileID: profile.ID,
		OwnerUserID:      &user.ID,
		Monitored:        true,
	}
	require.NoError(t, db.Create(&artist).Error)

	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	require.NoError(t, engine.LoadFromDir())
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	handler := NewArtistsHandler(db, services.NewArtistTrackingService(db, services.NewMusicBrainzService(nil)), nil)
	app.Get("/partials/artists", handler.RenderPartial)

	resp, err := app.Test(httptest.NewRequest("GET", "/partials/artists", nil))

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

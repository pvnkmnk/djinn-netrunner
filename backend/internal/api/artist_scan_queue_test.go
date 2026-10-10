package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-588 at the HTTP boundary. The ticket asked for "a test asserting the
// scan actually reaches the queue, since nothing asserts it today": the service
// test proves AddMonitoredArtist queues, this proves the route the operator
// actually calls does, because a route that silently stopped calling the
// service would leave the service test green forever.

func newArtistScanTestApp(t *testing.T) (*fiber.App, *gorm.DB, database.User) {
	t.Helper()

	// File-backed, not ":memory:": AddMonitoredArtist now writes the artist and
	// its job in one transaction, and a memory DSN gives each pooled connection
	// its own database (recorded in backend/internal/services/codemap.md).
	dsn := filepath.Join(t.TempDir(), "artist-scan.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "artist-scan-api@test.local", PasswordHash: "h", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "Default", IsDefault: true}
	require.NoError(t, db.Create(&profile).Error)

	stub := &stubMusicBrainz{byID: map[string]services.MusicBrainzArtist{
		"dji588-http-mbid": {ID: "dji588-http-mbid", Name: "Scan On Add", SortName: "Scan On Add"},
	}}
	handler := NewArtistsHandler(db,
		services.NewArtistTrackingService(db, nil),
		stub.asService())

	app := fiber.New()
	inject := func(c fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}
	app.Post("/api/artists", inject, handler.Add)
	app.Post("/api/artists/:id/sync", inject, handler.Sync)
	return app, db, user
}

func TestArtistsHandler_AddQueuesTheScanItPromises(t *testing.T) {
	app, db, user := newArtistScanTestApp(t)

	req := httptest.NewRequest("POST", "/api/artists",
		bytes.NewReader([]byte(`{"name":"Scan On Add","musicbrainz_id":"dji588-http-mbid"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var jobs []database.Job
	require.NoError(t, db.Where("job_type = ?", "artist_scan").Find(&jobs).Error)
	require.Len(t, jobs, 1, "a 201 from POST /api/artists must leave a scan on the queue, not just a row")
	assert.Equal(t, "queued", jobs[0].State)
	assert.Equal(t, "artist", jobs[0].ScopeType)
	assert.Equal(t, &user.ID, jobs[0].OwnerUserID)

	// The scan must be scoped to the artist this response created, or the
	// worker would scan something else while the Jobs page looks correct.
	var artist database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "dji588-http-mbid").First(&artist).Error)
	assert.Equal(t, artist.ID.String(), jobs[0].ScopeID)
}

// One owner, two callers. Add queued the scan, so the operator's on-demand Sync
// must answer THAT job. If Sync still carried its own copy of the enqueue, or
// pointed at a different scope, this would queue a second scan.
func TestArtistsHandler_SyncAnswersTheScanAddAlreadyQueued(t *testing.T) {
	app, db, _ := newArtistScanTestApp(t)

	req := httptest.NewRequest("POST", "/api/artists",
		bytes.NewReader([]byte(`{"name":"Scan On Add","musicbrainz_id":"dji588-http-mbid"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var queued database.Job
	require.NoError(t, db.Where("job_type = ?", "artist_scan").First(&queued).Error)

	var artist database.MonitoredArtist
	require.NoError(t, db.Where("music_brainz_id = ?", "dji588-http-mbid").First(&artist).Error)

	req = httptest.NewRequest("POST", "/api/artists/"+artist.ID.String()+"/sync", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "sync-already-active", resp.Header.Get("HX-Trigger"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "sync_already_active", body["status"])
	assert.EqualValues(t, queued.ID, body["job_id"],
		"Sync must hand back the job Add queued, not queue a second scan")

	var total int64
	require.NoError(t, db.Model(&database.Job{}).Where("job_type = ?", "artist_scan").Count(&total).Error)
	assert.Equal(t, int64(1), total, "Add and Sync share one enqueue owner, so one artist yields one scan")
}

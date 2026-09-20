package api

// Every htmx form in ops/web/templates posts
// `application/x-www-form-urlencoded`. The handlers' input structs carried only
// `json` tags, so those bodies bound to zero values: the watchlist modal 400'd
// with `unsupported source type: ` — the first thing a new user does — and the
// sibling create forms (library, profile, schedule) failed the same way.
//
// Each case below posts the body its own modal sends, underscored field names
// included, and asserts the *bound values* reached the handler. Dropping a
// `form` tag makes the case fail, not silently pass.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// authedFormApp mounts routes behind a Locals-injecting stand-in for
// AuthMiddleware, mirroring what the middleware puts in Locals for real.
func authedFormApp(user database.User, mount func(*fiber.App)) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	mount(app)
	return app
}

func formUser(t *testing.T, db *gorm.DB) database.User {
	t.Helper()
	user := database.User{Email: "form-probe@example.com", PasswordHash: "xxx", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	return user
}

// submitForm posts a form-encoded body exactly as htmx does for an hx-post form.
func submitForm(t *testing.T, app *fiber.App, method, path string, fields map[string]string) *http.Response {
	t.Helper()
	body := url.Values{}
	for k, v := range fields {
		body.Set(k, v)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

// respBody reads and logs the response so a failing case shows what the handler
// actually said instead of only the follow-up assertion.
func respBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("status=%d body=%s", resp.StatusCode, string(b))
	return string(b)
}

// TestWatchlistCreate_FormEncodedBody covers the Add Watchlist modal. The
// Quality Profile select submits an empty value whenever the user leaves it on
// the placeholder, which is the default. That value must bind (an empty string
// cannot unmarshal into a uuid.UUID, and that failing rejected the whole body)
// and must resolve to the global default profile: the row carries a foreign key
// that Postgres enforces, so the zero UUID cannot be stored. SQLite does not
// enforce foreign keys, which is precisely how that reached the live stack.
func TestWatchlistCreate_FormEncodedBody(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	defaultProfile := database.QualityProfile{Name: "Global Default", IsDefault: true}
	require.NoError(t, db.Create(&defaultProfile).Error)
	service := services.NewWatchlistService(db, nil, &config.Config{})
	handler := NewWatchlistHandler(db, service)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Post("/api/watchlists", handler.CreateWatchlist)
	})

	resp := submitForm(t, app, "POST", "/api/watchlists", map[string]string{
		"name":               "Probe Feed",
		"source_type":        "rss_feed",
		"source_uri":         "https://example.com/probe-feed.xml",
		"quality_profile_id": "",
		"enabled":            "on",
	})
	body := respBody(t, resp)
	assert.NotEqual(t, 400, resp.StatusCode, "form-encoded create must bind, not reject the body: "+body)

	var stored database.Watchlist
	require.NoError(t, db.Where("source_uri = ?", "https://example.com/probe-feed.xml").First(&stored).Error)
	assert.Equal(t, "Probe Feed", stored.Name)
	assert.Equal(t, "rss_feed", stored.SourceType)
	assert.Equal(t, defaultProfile.ID, stored.QualityProfileID, "an empty selection resolves to the global default profile")
}

// TestWatchlistUpdate_FormEncodedBody covers the edit modal, which PATCHes the
// item's own URL. This is the path the modal could never reach.
func TestWatchlistUpdate_FormEncodedBody(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)

	defaultProfile := database.QualityProfile{Name: "Global Default", IsDefault: true}
	require.NoError(t, db.Create(&defaultProfile).Error)

	existing := database.Watchlist{
		Name: "Original", SourceType: "rss_feed",
		SourceURI: "https://example.com/edit-me.xml", Enabled: true, OwnerUserID: &user.ID,
		QualityProfileID: defaultProfile.ID,
	}
	require.NoError(t, db.Create(&existing).Error)

	service := services.NewWatchlistService(db, nil, &config.Config{})
	handler := NewWatchlistHandler(db, service)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Patch("/api/watchlists/:id", handler.UpdateWatchlist)
	})

	resp := submitForm(t, app, "PATCH", "/api/watchlists/"+existing.ID.String(), map[string]string{
		"name":               "Renamed",
		"source_type":        "rss_feed",
		"source_uri":         "https://example.com/edit-me.xml",
		"quality_profile_id": "",
		"enabled":            "on",
	})
	body := respBody(t, resp)
	assert.NotEqual(t, 400, resp.StatusCode, "the edit modal's PATCH must bind its form body: "+body)
	// The edit came from the modal, so the response has to close it - the same
	// header the create path already sets.
	assert.Equal(t, "closeModal", resp.Header.Get("HX-Trigger"), "the edit modal still has to close")

	var stored database.Watchlist
	require.NoError(t, db.First(&stored, "id = ?", existing.ID).Error)
	assert.Equal(t, "Renamed", stored.Name, "bound name must reach the update")
}

// TestLibraryCreate_FormEncodedBody covers the Add Library modal.
func TestLibraryCreate_FormEncodedBody(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	handler := NewLibraryHandler(db)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Post("/api/libraries", handler.CreateLibrary)
	})

	libPath := t.TempDir()
	resp := submitForm(t, app, "POST", "/api/libraries", map[string]string{
		"name": "Probe Library",
		"path": libPath,
	})
	assert.NotEqual(t, 400, resp.StatusCode, "form-encoded create must bind, not reject the body")

	var stored database.Library
	require.NoError(t, db.Where("name = ?", "Probe Library").First(&stored).Error)
	assert.Equal(t, "Probe Library", stored.Name)
	assert.NotEmpty(t, stored.Path)
}

// TestProfileCreate_FormEncodedBody covers the Add Profile modal, whose fields
// are mostly underscored (prefer_lossless, min_bitrate, ...).
func TestProfileCreate_FormEncodedBody(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	handler := NewProfileHandler(db)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Post("/api/profiles", handler.Create)
	})

	resp := submitForm(t, app, "POST", "/api/profiles", map[string]string{
		"name":                  "Probe Profile",
		"description":           "created by the form binding test",
		"min_bitrate":           "320",
		"allowed_formats":       "flac",
		"prefer_lossless":       "on",
		"prefer_scene_releases": "on",
		"prefer_web_releases":   "on",
		"cover_art_sources":     "embedded",
	})
	assert.NotEqual(t, 400, resp.StatusCode, "form-encoded create must bind, not reject the body")

	var stored database.QualityProfile
	require.NoError(t, db.Where("name = ?", "Probe Profile").First(&stored).Error)
	assert.Equal(t, "Probe Profile", stored.Name)
	assert.True(t, stored.PreferLossless, "checkbox field must bind from the form")
}

// TestScheduleCreate_FormEncodedBody covers the Add Schedule modal, whose
// watchlist_id is underscored and uuid-valued.
func TestScheduleCreate_FormEncodedBody(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)

	watchlist := database.Watchlist{
		Name: "Scheduled", SourceType: "rss_feed",
		SourceURI: "https://example.com/scheduled.xml", Enabled: true, OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&watchlist).Error)

	handler := NewSchedulesHandler(db)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Post("/api/schedules", handler.Create)
	})

	resp := submitForm(t, app, "POST", "/api/schedules", map[string]string{
		"watchlist_id": watchlist.ID.String(),
		"cron_expr":    "0 3 * * *",
		"enabled":      "on",
	})
	assert.NotEqual(t, 400, resp.StatusCode, "form-encoded create must bind, not reject the body")

	var stored database.Schedule
	require.NoError(t, db.Where("cron_expr = ?", "0 3 * * *").First(&stored).Error)
	assert.Equal(t, watchlist.ID, stored.WatchlistID)
}

// TestProfileUpdate_FormClearsUncheckedFlags covers the other half of form
// binding: a form omits unchecked checkboxes, so an absent flag has to mean
// "off" for a form PATCH. Without that, a profile's flags could be turned on
// but never off again from the UI. JSON PATCH keeps nil = leave unchanged.
func TestProfileUpdate_FormClearsUncheckedFlags(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	// Owned by the caller: a non-admin can only update their own profile, and an
	// unowned fixture would 404 before any of this is exercised.
	profile := database.QualityProfile{
		Name: "Flagged", PreferLossless: true, PreferSceneReleases: true,
		PreferWebReleases: true, OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&profile).Error)

	handler := NewProfileHandler(db)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Patch("/api/profiles/:id", handler.Update)
	})

	resp := submitForm(t, app, "PATCH", "/api/profiles/"+profile.ID.String(), map[string]string{
		"name": "Flagged",
	})
	body := respBody(t, resp)
	require.Equal(t, 200, resp.StatusCode, "the update has to reach the profile: "+body)

	var stored database.QualityProfile
	require.NoError(t, db.First(&stored, "id = ?", profile.ID).Error)
	assert.False(t, stored.PreferLossless, "an unchecked box must clear the flag")
	assert.False(t, stored.PreferSceneReleases, "an unchecked box must clear the flag")
	assert.False(t, stored.PreferWebReleases, "an unchecked box must clear the flag")
}

// TestScheduleUpdate_FormClearsUncheckedEnabled is the schedule form's version
// of the same rule: leaving "Enabled" unticked must disable the schedule.
func TestScheduleUpdate_FormClearsUncheckedEnabled(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)

	watchlist := database.Watchlist{
		Name: "Toggle", SourceType: "rss_feed",
		SourceURI: "https://example.com/toggle.xml", Enabled: true, OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&watchlist).Error)
	schedule := database.Schedule{
		WatchlistID: watchlist.ID, CronExpr: "0 1 * * *", Timezone: "UTC", Enabled: true,
	}
	require.NoError(t, db.Create(&schedule).Error)

	handler := NewSchedulesHandler(db)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Patch("/api/schedules/:id", handler.Update)
	})

	resp := submitForm(t, app, "PATCH", "/api/schedules/"+strconv.FormatUint(uint64(schedule.ID), 10), map[string]string{
		"cron_expr": "0 2 * * *",
	})
	assert.NotEqual(t, 400, resp.StatusCode)

	var stored database.Schedule
	require.NoError(t, db.First(&stored, "id = ?", schedule.ID).Error)
	assert.False(t, stored.Enabled, "an unticked Enabled box must disable the schedule")
}

// TestArtistAdd_NoDefaultProfile_SaysSo covers the artist form's dependence on
// a default quality profile: the zero UUID must not reach the foreign key, and
// the reason has to be actionable. The MusicBrainz lookup runs after this
// branch, so the case needs no network.
func TestArtistAdd_NoDefaultProfile_SaysSo(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	handler := NewArtistsHandler(db, nil, nil)
	app := authedFormApp(user, func(app *fiber.App) {
		app.Post("/api/artists", handler.Add)
	})

	resp := submitForm(t, app, "POST", "/api/artists", map[string]string{"name": "Nobody"})
	body := respBody(t, resp)
	assert.Equal(t, 400, resp.StatusCode, "no default profile must be reported, not a DB error: "+body)
	assert.Contains(t, body, "default quality profile")
}

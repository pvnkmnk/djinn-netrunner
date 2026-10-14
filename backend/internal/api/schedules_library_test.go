package api

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI library scans were only ever run by hand: `POST /api/libraries/:id/scan`
// queues one, and nothing queued another tomorrow. A schedule now names the job
// type it runs, and the job type names the scope, so "scan" plus a library_id is
// a library's scan on a cron while "sync" plus a watchlist_id stays what a
// schedule has always been.
//
// The pair is validated, not assumed: a row that names a watchlist and queues a
// library job would run against whatever the library id happened to be, which is
// why the column the job type cannot use is refused rather than ignored.

// scheduleTestApp mounts the schedules routes for one caller. The template
// engine is loaded because Toggle answers with the card partial: without Views
// an unimplemented render comes back as a 500 that looks like a handler bug.
func scheduleTestApp(t *testing.T, db *gorm.DB, user database.User) *fiber.App {
	t.Helper()
	handler := NewSchedulesHandler(db)
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Get("/api/schedules", handler.List)
	app.Post("/api/schedules", handler.Create)
	app.Patch("/api/schedules/:id", handler.Update)
	app.Patch("/api/schedules/:id/toggle", handler.Toggle)
	app.Delete("/api/schedules/:id", handler.Delete)
	return app
}

// scheduleFixture builds the two scopes a schedule can name, both owned by the
// same account.
func scheduleFixture(t *testing.T, db *gorm.DB, user database.User) (database.Watchlist, database.Library) {
	t.Helper()
	profile := database.QualityProfile{Name: "Schedule Scope " + uuid.NewString(), IsDefault: true}
	require.NoError(t, db.Create(&profile).Error)
	watchlist := database.Watchlist{
		ID: uuid.New(), Name: "Scope Watchlist " + uuid.NewString(), SourceType: "rss_feed",
		SourceURI: "https://example.test/scope.xml", QualityProfileID: profile.ID, OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&watchlist).Error)
	library := database.Library{ID: uuid.New(), Name: "Scope Library", Path: filepath.Clean(t.TempDir()), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&library).Error)
	return watchlist, library
}

func TestScheduleCreate_TheJobTypeNamesTheScope(t *testing.T) {
	db := setupAPITestDB(t)
	user := formUser(t, db)
	watchlist, library := scheduleFixture(t, db, user)
	app := scheduleTestApp(t, db, user)

	// The acceptance criterion: a library's scan can be scheduled at all.
	created := submitForm(t, app, "POST", "/api/schedules", map[string]string{
		"job_type": "scan", "library_id": library.ID.String(), "cron_expr": "0 4 * * *", "enabled": "on",
	})
	require.Equal(t, 201, created.StatusCode, respBody(t, created))

	var stored database.Schedule
	require.NoError(t, db.Where("cron_expr = ?", "0 4 * * *").First(&stored).Error)
	require.NotNil(t, stored.LibraryID, "a scan schedule's scope is the library it was created with")
	assert.Equal(t, library.ID, *stored.LibraryID)
	assert.Nil(t, stored.WatchlistID, "and it must not also carry a watchlist")
	assert.Equal(t, "scan", stored.JobType)
	assert.True(t, stored.Enabled)

	// Enrich is the other library job, and the same shape.
	enrich := submitForm(t, app, "POST", "/api/schedules", map[string]string{
		"job_type": "enrich", "library_id": library.ID.String(), "cron_expr": "0 5 * * *", "enabled": "on",
	})
	require.Equal(t, 201, enrich.StatusCode, respBody(t, enrich))

	// A sync still means what it always meant: an omitted job type is the form
	// that predates library schedules, and it posts a watchlist.
	sync := submitForm(t, app, "POST", "/api/schedules", map[string]string{
		"watchlist_id": watchlist.ID.String(), "cron_expr": "0 6 * * *", "enabled": "on",
	})
	require.Equal(t, 201, sync.StatusCode, respBody(t, sync))

	refused := []struct {
		name    string
		fields  map[string]string
		message string
	}{
		{
			"a sync cannot name a library",
			map[string]string{"job_type": "sync", "library_id": library.ID.String()},
			"runs against a watchlist",
		},
		{
			"a scan cannot name a watchlist",
			map[string]string{"job_type": "scan", "watchlist_id": watchlist.ID.String()},
			"runs against a library",
		},
		{
			"a job type the worker does not dispatch is refused",
			map[string]string{"job_type": "acquisition", "library_id": library.ID.String()},
			"unschedulable job_type",
		},
		{
			"a scan without a scope says which id it wanted",
			map[string]string{"job_type": "scan"},
			"invalid library_id",
		},
		{
			"an unparseable library id is refused",
			map[string]string{"job_type": "scan", "library_id": "library-1"},
			"invalid library_id",
		},
		{
			"an unparseable watchlist id is refused",
			map[string]string{"watchlist_id": "watchlist-1"},
			"invalid watchlist_id",
		},
		{
			"a zero watchlist id is refused rather than stored",
			map[string]string{"watchlist_id": "00000000-0000-0000-0000-000000000000"},
			"invalid watchlist_id",
		},
		{
			"a zero library id is refused rather than stored",
			map[string]string{"job_type": "scan", "library_id": "00000000-0000-0000-0000-000000000000"},
			"invalid library_id",
		},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{"cron_expr": "0 4 * * *", "enabled": "on"}
			for key, value := range tc.fields {
				fields[key] = value
			}
			resp := submitForm(t, app, "POST", "/api/schedules", fields)
			body := respBody(t, resp)
			assert.Equal(t, 400, resp.StatusCode, body)
			assert.Contains(t, body, tc.message)
		})
	}

	// Three schedules were accepted and every refusal was refused: a 400 that
	// still stored the row would be the failure this count exists to catch.
	var count int64
	require.NoError(t, db.Model(&database.Schedule{}).Count(&count).Error)
	assert.EqualValues(t, 3, count, "a refused schedule must not be stored")
}

func TestScheduleCreate_NonAdminCannotScheduleAStrangerScope(t *testing.T) {
	db := setupAPITestDB(t)
	owner := formUser(t, db)
	_, library := scheduleFixture(t, db, owner)
	stranger := database.User{Email: "schedule-stranger@example.com", PasswordHash: "x", Role: "user"}
	require.NoError(t, db.Create(&stranger).Error)

	app := scheduleTestApp(t, db, stranger)
	resp := submitForm(t, app, "POST", "/api/schedules", map[string]string{
		"job_type": "scan", "library_id": library.ID.String(), "cron_expr": "0 4 * * *",
	})
	body := respBody(t, resp)
	assert.Equal(t, 403, resp.StatusCode, body)
	assert.Contains(t, body, "you do not own this library")

	var count int64
	require.NoError(t, db.Model(&database.Schedule{}).Count(&count).Error)
	assert.Zero(t, count)
}

// TestSchedulesOwnership_CoversLibraryScopes is the guard for the ownership
// filter. It used to JOIN `watchlists` on schedules.watchlist_id, which can only
// ever see one of the two scopes: a library schedule would have been missing
// from its owner's list, and -- because the same JOIN guarded update, toggle and
// delete -- its owner could not have changed one either.
func TestSchedulesOwnership_CoversLibraryScopes(t *testing.T) {
	db := setupAPITestDB(t)
	owner := formUser(t, db)
	_, library := scheduleFixture(t, db, owner)
	stranger := database.User{Email: "library-schedule-stranger@example.com", PasswordHash: "x", Role: "user"}
	require.NoError(t, db.Create(&stranger).Error)

	sched := database.Schedule{
		JobType: "enrich", LibraryID: &library.ID,
		CronExpr: "0 5 * * *", Timezone: "UTC", Enabled: true,
	}
	require.NoError(t, db.Create(&sched).Error)
	id := strconv.FormatUint(sched.ID, 10)

	ownerApp := scheduleTestApp(t, db, owner)
	strangerApp := scheduleTestApp(t, db, stranger)

	ownerList := respBody(t, submitForm(t, ownerApp, "GET", "/api/schedules", nil))
	assert.Contains(t, ownerList, `"JobType":"enrich"`, "the owner must see their own library schedule")
	assert.Contains(t, ownerList, library.ID.String())

	strangerList := respBody(t, submitForm(t, strangerApp, "GET", "/api/schedules", nil))
	assert.Equal(t, "[]", strings.TrimSpace(strangerList), "a stranger must not see it at all")

	// And the same rule on every mutating control, not just the list. Toggle
	// answers 404 rather than 403 on purpose -- it reports "not found" rather
	// than confirming that someone else's schedule exists.
	assert.Equal(t, 404, submitForm(t, strangerApp, "PATCH", "/api/schedules/"+id+"/toggle", nil).StatusCode)
	assert.Equal(t, 403, submitForm(t, strangerApp, "DELETE", "/api/schedules/"+id, nil).StatusCode)
	assert.Equal(t, 403, submitForm(t, strangerApp, "PATCH", "/api/schedules/"+id, map[string]string{"enabled": "false"}).StatusCode)

	var untouched database.Schedule
	require.NoError(t, db.First(&untouched, sched.ID).Error)
	assert.True(t, untouched.Enabled, "a refused toggle must not have applied")

	// The owner may, which is what proves the filter is a filter and not a wall.
	assert.Equal(t, 200, submitForm(t, ownerApp, "PATCH", "/api/schedules/"+id+"/toggle", nil).StatusCode)
	var toggled database.Schedule
	require.NoError(t, db.First(&toggled, sched.ID).Error)
	assert.False(t, toggled.Enabled)

	assert.Equal(t, 200, submitForm(t, ownerApp, "DELETE", "/api/schedules/"+id, nil).StatusCode)
	var remaining int64
	require.NoError(t, db.Model(&database.Schedule{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

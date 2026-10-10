package api

import (
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDashboardHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	assert.NotNil(t, &DashboardHandler{})
}

func TestDashboardHandler_NewDashboardHandler(t *testing.T) {
	// Test that NewDashboardHandler returns a non-nil handler
	handler := NewDashboardHandler(nil)
	assert.NotNil(t, handler, "expected non-nil handler")

	// Verify the db field is set (even if nil)
	var db *gorm.DB
	assert.Equal(t, db, handler.db)
}

// TestRenderIndex_FooterShowsVersion pins the version in the footer of the
// page every signed-in user lands on. It rendered empty there while every
// other page showed it, because RenderIndex called c.Render directly
// instead of going through RenderPage, which is what supplies Version from
// AppVersion. Rendering through the real engine is what makes this bite.
func TestRenderIndex_FooterShowsVersion(t *testing.T) {
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")

	app := fiber.New(fiber.Config{Views: engine})
	app.Use(withUser(database.User{ID: 1, Email: "footer@test.com", Role: "user"}))
	app.Get("/", (&DashboardHandler{}).RenderIndex)

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), "NetRunner v"+AppVersion,
		"the dashboard footer must name the running version")
}

// TestAppVersion_DefaultIsNotARelease pins the source default. The version is
// supplied at build time from the release tag (APP_VERSION -> -ldflags -X), so
// what is compiled into the source must stay a non-release sentinel: a
// hardcoded "0.1.0" here is what made v0.1.1 render "NetRunner v0.1.0" in the
// footer of every page while claiming to be v0.1.1.
func TestAppVersion_DefaultIsNotARelease(t *testing.T) {
	assert.Equal(t, "dev", AppVersion,
		"AppVersion must be build-injected; keep the source default a non-release sentinel")
}

// dashboardTestApp builds the dashboard route with the real templates, the way
// a signed-in user gets it.
func dashboardTestApp(t *testing.T, db *gorm.DB, user *database.User) *fiber.App {
	t.Helper()
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	h := NewDashboardHandler(db)
	app.Get("/", func(c fiber.Ctx) error {
		if user != nil {
			c.Locals("user", *user)
		}
		return h.RenderIndex(c)
	})
	return app
}

// The defect: the dashboard shipped a hard-coded "Loading stats..." placeholder
// and triggered only on "every 30s" / "every 60s" with no load trigger, so the
// first real content arrived half a minute to a minute after the page. This
// asserts the first server-rendered response carries the content itself, not
// only the containers.
func TestRenderIndex_FirstResponseCarriesStatsAndWatchlists(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	// One queued and one succeeded job, so the numbers are distinguishable from
	// an all-zero region that came from a query which simply found nothing.
	now := time.Now()
	queued := database.Job{Type: "scan", State: "queued", RequestedAt: now, OwnerUserID: &user.ID}
	done := database.Job{Type: "sync", State: "succeeded", RequestedAt: now, OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&queued).Error)
	require.NoError(t, db.Create(&done).Error)

	wl := database.Watchlist{Name: "Road Trip", SourceType: "spotify_playlist", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&wl).Error)

	resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	html := body(t, resp)

	assert.NotContains(t, html, "Loading stats...", "the placeholder is the defect, not the fix")
	assert.NotContains(t, html, "Loading watchlists...")

	assert.Contains(t, html, "stat-card", "the stats region must be rendered, not just a container")
	assert.Contains(t, html, "Queued", "the stat labels are part of the content")
	assert.Contains(t, html, "Road Trip", "the watchlists region must be rendered in the first response")
}

// The whole point of the fix: a user who genuinely has nothing sees a real
// answer, not a spinner they cannot tell from a broken page.
func TestRenderIndex_EmptyAccountShowsAnEmptyStateNotALoadingState(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := body(t, resp)

	assert.NotContains(t, html, "Loading stats...")
	assert.NotContains(t, html, "Loading watchlists...")
	assert.Contains(t, html, "No watchlists configured",
		"an account with no watchlists must be told so in the first response")
	assert.Contains(t, html, "Add Watchlist", "the empty state has to offer the action that creates one")
}

// A failed count is not zero. Rendering four confident zeroes after a query
// error is the same class of lie this slice exists to remove.
// One broken region must not take the other one down with it. RenderIndex
// merges both builders into a single render context and a pongo2 include
// inherits the parent context, so a shared error key meant a watchlists
// failure also erased the stat cards that had rendered fine - the exact
// failure this slice is about, reappearing one layer up.
func TestRenderIndex_OneFailingRegionDoesNotBlankTheOther(t *testing.T) {
	tests := []struct {
		name      string
		failTable string
		wantError string
		wantGone  string
		wantKept  string
	}{
		{
			name:      "watchlists query fails, stats still render",
			failTable: "watchlists",
			wantError: "Error loading watchlists.",
			wantGone:  "Road Trip",
			wantKept:  "stat-card",
		},
		{
			name:      "jobs query fails, watchlists still render",
			failTable: "jobs",
			wantError: "Error loading stats.",
			wantGone:  "stat-value",
			wantKept:  "Road Trip",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A shared-cache in-memory database, because a plain
			// ":memory:" belongs to a single connection: when the pool
			// opens a second one it sees no tables at all, which looks
			// exactly like the query failure injected below.
			dsn := fmt.Sprintf("file:dji549_%s?mode=memory&cache=shared", t.Name())
			db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, database.Migrate(db))

			user := database.User{Email: "onefail@test.com", PasswordHash: "xxx", Role: "user"}
			require.NoError(t, db.Create(&user).Error)

			now := time.Now()
			queued := database.Job{Type: "scan", State: "queued", RequestedAt: now, OwnerUserID: &user.ID}
			require.NoError(t, db.Create(&queued).Error)

			wl := database.Watchlist{Name: "Road Trip", SourceType: "spotify_playlist", OwnerUserID: &user.ID}
			require.NoError(t, db.Create(&wl).Error)

			// Fail exactly one table, so the surviving region is a real
			// success rather than a second failure. Both processors are
			// registered because the counts arrive through Scan, which is
			// a Row callback, while the watchlists arrive through Find.
			failOne := func(tx *gorm.DB) {
				if tx.Statement.Table == tc.failTable {
					tx.AddError(errors.New("query failed"))
				}
			}
			name := "fail_" + tc.failTable
			require.NoError(t, db.Callback().Query().Register(name, failOne))
			require.NoError(t, db.Callback().Row().Register(name, failOne))

			resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			html := body(t, resp)

			assert.Contains(t, html, tc.wantError, "the failed region says so")
			assert.NotContains(t, html, tc.wantGone, "the failed region must not render content")
			assert.Contains(t, html, tc.wantKept, "the healthy region must still render")
		})
	}
}

// A failed count is not zero. Rendering four confident zeroes after a query
// error is the same class of lie this slice exists to remove.
func TestRenderIndex_FailedStatsCountIsNotRenderedAsZeroes(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	// Close the pool: the queries then fail for real rather than being stubbed,
	// so the assertion is about what the template does with a failure.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := body(t, resp)

	assert.Contains(t, html, "Error loading")
	assert.NotContains(t, html, "stat-value",
		"a failed query must not render as four zeroes")
}

// The sign-in screen is served from this same template, so the dashboard's
// server-rendering must not leak owner-scoped numbers onto it.
func TestRenderIndex_SignedOutGetsTheSignInScreenAndNoNumbers(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	resp, err := dashboardTestApp(t, db, nil).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := body(t, resp)

	assert.Contains(t, html, "Sign In", "an unauthenticated visitor gets the sign-in card")
	assert.NotContains(t, html, "Road Trip", "no owner's data on the sign-in screen")
	assert.NotContains(t, html, "Loading stats...")
}

// Periodic refresh has to survive server-rendering, or the dashboard would go
// stale forever the moment the page loads.
func TestRenderIndex_RegionsKeepTheirRefreshTriggers(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := body(t, resp)

	assert.Contains(t, html, `hx-get="/partials/stats"`)
	assert.Contains(t, html, `hx-get="/partials/watchlists"`)
	assert.Contains(t, html, "every 30s")
	assert.Contains(t, html, "every 60s")
}

// .stats-region is display:grid with an auto-fit track list. A section carrying
// that class around the partial - which is itself .stats-region - nests a grid
// inside a grid and collapses the stat cards to one column. The page must not
// reintroduce the class on the swap target.
func TestRenderIndex_StatsRegionIsNotNestedInsideItself(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	resp, err := dashboardTestApp(t, db, &user).Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := body(t, resp)

	assert.Equal(t, 1, strings.Count(html, `class="stats-region"`),
		"exactly one .stats-region element, or the auto-fit grid collapses")
}

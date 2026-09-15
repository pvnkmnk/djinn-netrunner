package main

import (
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestHealthCheck(t *testing.T) {
	app := fiber.New()
	// Create mock or nil handlers as needed
	artistsHandler := &api.ArtistsHandler{}
	schedulesHandler := &api.SchedulesHandler{}
	cfg := &config.Config{
		AuthRateLimitMax:        10,
		AuthRateLimitExpiration: "1m",
	}
	healthHandler := api.NewHealthHandler(nil, cfg)
	app.Get("/api/health", healthHandler.GetHealth)
	acquireHandler := &api.AcquireHandler{}
	setupRoutes(app, nil, cfg, &api.AuthHandler{}, &api.DashboardHandler{}, &api.StatsHandler{}, &api.LibraryHandler{}, &api.ProfileHandler{}, &api.WatchlistHandler{}, &services.WatchlistService{}, &api.SpotifyAuthHandler{}, &api.WebSocketManager{}, &services.ArtistTrackingService{}, &services.ScannerService{}, artistsHandler, schedulesHandler, acquireHandler, &api.AdminHandler{}, &api.PlaylistHandler{})

	resp, err := app.Test(httptest.NewRequest("GET", "/api/health", nil))
	assert.NoError(t, err)
	assert.Equal(t, 503, resp.StatusCode)
}

func TestListenAddressUsesConfiguredPort(t *testing.T) {
	assert.Equal(t, ":18080", listenAddress(&config.Config{Port: "18080"}))
	assert.Equal(t, ":8080", listenAddress(&config.Config{}))
}
// --- Route table -----------------------------------------------------------
//
// setupRoutes registers every route, so the table can be asserted directly
// instead of probing endpoints. That matters for the Subsonic surface: its
// routes all carry a `.view` suffix, and a bare `/rest/getIndexes` answers
// "Cannot GET" — which reads like a route that was never registered rather
// than a naming rule that was not followed.

// routeTable indexes registered routes as "METHOD /path".
func routeTable(app *fiber.App) map[string]bool {
	table := make(map[string]bool, len(app.GetRoutes()))
	for _, r := range app.GetRoutes() {
		table[r.Method+" "+r.Path] = true
	}
	return table
}

func baseRouteTestConfig() *config.Config {
	return &config.Config{
		AuthRateLimitMax:        10,
		AuthRateLimitExpiration: "1m",
	}
}

func newRouteTestApp(t *testing.T, cfg *config.Config) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	artistsHandler := &api.ArtistsHandler{}
	schedulesHandler := &api.SchedulesHandler{}
	acquireHandler := &api.AcquireHandler{}
	setupRoutes(app, nil, cfg, &api.AuthHandler{}, &api.DashboardHandler{}, &api.StatsHandler{}, &api.LibraryHandler{}, &api.ProfileHandler{}, &api.WatchlistHandler{}, &services.WatchlistService{}, &api.SpotifyAuthHandler{}, &api.WebSocketManager{}, &services.ArtistTrackingService{}, &services.ScannerService{}, artistsHandler, schedulesHandler, acquireHandler, &api.AdminHandler{}, &api.PlaylistHandler{})
	return app
}

func TestSetupRoutes_RegistersCriticalRoutes(t *testing.T) {
	table := routeTable(newRouteTestApp(t, baseRouteTestConfig()))

	// One per prefix family, picked where a wrong prefix or verb fails quietly
	// as a 404: the browser's auth calls, the pages the nav links to, and the
	// agent-facing API the CLI and the worker drive.
	critical := []string{
		"POST /api/auth/login",
		"POST /api/auth/logout",
		"GET /api/auth/spotify/callback",
		"POST /api/auth/spotify/spdc",
		"POST /api/acquire",
		"POST /api/watchlists/:id/sync",
		"POST /api/libraries/:id/scan",
		"GET /api/libraries/:id/tracks",
		"POST /api/artists/:id/sync",
		"POST /api/jobs/:id/retry",
		"POST /api/jobs/:id/cancel",
		"GET /api/stats/jobs",
		"PATCH /api/admin/config",
		"GET /",
		"GET /jobs",
		"GET /partials/artists",
		"GET /tracks/:id/stream",
		"GET /ws/events",
	}

	for _, want := range critical {
		if !table[want] {
			t.Errorf("route %q is not registered", want)
		}
	}
}

func TestSetupRoutes_SubsonicRoutesNeedViewSuffix(t *testing.T) {
	cfg := baseRouteTestConfig()
	cfg.Subsonic.Enabled = true
	cfg.Subsonic.Password = "test-subsonic-password"
	table := routeTable(newRouteTestApp(t, cfg))

	if !table["GET /rest/getIndexes.view"] {
		t.Error("/rest/getIndexes.view is not registered")
	}
	if table["GET /rest/getIndexes"] {
		t.Error("bare /rest/getIndexes must not be registered — Subsonic routes need the .view suffix")
	}

	// Asserted across the whole family so the next endpoint added without the
	// suffix fails here rather than inside a Subsonic client.
	var bare []string
	for route := range table {
		method, path, ok := strings.Cut(route, " ")
		if !ok || !strings.HasPrefix(path, "/rest/") {
			continue
		}
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		default:
			continue // group middleware registers as USE, not an endpoint
		}
		if !strings.HasSuffix(path, ".view") {
			bare = append(bare, route)
		}
	}
	sort.Strings(bare)
	if len(bare) > 0 {
		t.Errorf("every /rest endpoint must end in .view; missing on: %v", bare)
	}
}

func TestSetupRoutes_SubsonicAbsentWhenDisabled(t *testing.T) {
	// With Subsonic off the endpoint must not exist at all — otherwise
	// "disabled" is only a check inside the handler, and the surface still
	// answers requests.
	cfg := baseRouteTestConfig()
	cfg.Subsonic.Enabled = false
	table := routeTable(newRouteTestApp(t, cfg))

	var registered []string
	for route := range table {
		if strings.Contains(route, " /rest/") {
			registered = append(registered, route)
		}
	}
	sort.Strings(registered)
	if len(registered) > 0 {
		t.Errorf("no /rest route may be registered when Subsonic is disabled: %v", registered)
	}
}

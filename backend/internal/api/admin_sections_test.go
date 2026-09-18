package api

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAdminPageApp builds an app that renders real templates, which the auth
// tests deliberately do not need: this file is about what the panel puts on the
// page for a given URL.
func setupAdminPageApp(t *testing.T, db *gorm.DB) *fiber.App {
	t.Helper()

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})

	handler := NewAdminHandler(db)
	injectUser := func(c *fiber.Ctx) error {
		// ID must be non-zero: currentUserFromLocals treats a zero ID as absent.
		c.Locals("user", database.User{ID: 1, Email: "admin@example.com", Role: "admin"})
		return c.Next()
	}

	app.Get("/admin", injectUser, handler.AdminOnly, handler.AdminPage)
	app.Get("/partials/admin/users", injectUser, handler.AdminOnly, handler.RenderUsersPartial)
	app.Get("/partials/admin/audit", injectUser, handler.AdminOnly, handler.RenderAuditPartial)
	app.Get("/partials/admin/config", injectUser, handler.AdminOnly, handler.RenderConfigPartial)
	return app
}

func getBody(t *testing.T, app *fiber.App, path string) string {
	t.Helper()

	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode, "%s must render", path)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

// The nav used to push the request URL (/partials/admin/users), which is not a
// page route, so reloading or bookmarking the panel 404'd. The pushed URL has to
// name the page.
func TestAdminNav_PushesPageURLs(t *testing.T) {
	app := setupAdminPageApp(t, setupAdminTestDB(t))
	body := getBody(t, app, "/admin")

	assert.NotContains(t, body, `hx-push-url="true"`,
		"hx-push-url=\"true\" pushes the request URL, which is a partial, not a page")
	assert.NotContains(t, body, `hx-push-url="/partials/`,
		"the pushed URL must not be a partial endpoint")
	assert.Contains(t, body, `hx-push-url="/admin?section=audit"`)
	assert.Contains(t, body, `hx-push-url="/admin?section=config"`)
}

// A section URL is what the nav now pushes, so it has to render that section
// server-side. Before this, /admin?section=audit answered 200 with an empty panel.
func TestAdminPage_RendersSelectedSection(t *testing.T) {
	app := setupAdminPageApp(t, setupAdminTestDB(t))

	tests := []struct {
		name     string
		path     string
		heading  string
		notThese []string
	}{
		{"default", "/admin", "<h3>Users</h3>", []string{"<h3>Audit Log</h3>", "<h3>System Config</h3>"}},
		{"users", "/admin?section=users", "<h3>Users</h3>", []string{"<h3>Audit Log</h3>", "<h3>System Config</h3>"}},
		{"audit", "/admin?section=audit", "<h3>Audit Log</h3>", []string{"<h3>System Config</h3>"}},
		{"config", "/admin?section=config", "<h3>System Config</h3>", []string{"<h3>Audit Log</h3>"}},
		// A hand-edited or stale URL renders the panel rather than nothing.
		{"unknown falls back", "/admin?section=nope", "<h3>Users</h3>", []string{"<h3>Audit Log</h3>"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := getBody(t, app, tt.path)
			assert.Contains(t, body, tt.heading)
			for _, absent := range tt.notThese {
				assert.NotContains(t, body, absent)
			}
		})
	}
}

// htmx swaps a partial into the panel; a reload renders the same section from the
// page. Both come from AdminHandler.adminSection, and this asserts they cannot
// drift: whatever the partial endpoint returns must be inside the page's markup.
func TestAdminPage_SectionMatchesThePartialEndpoint(t *testing.T) {
	app := setupAdminPageApp(t, setupAdminTestDB(t))

	tests := []struct {
		section     string
		pagePath    string
		partialPath string
	}{
		{"users", "/admin?section=users", "/partials/admin/users"},
		{"audit", "/admin?section=audit", "/partials/admin/audit"},
		{"config", "/admin?section=config", "/partials/admin/config"},
	}

	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			page := getBody(t, app, tt.pagePath)
			partial := getBody(t, app, tt.partialPath)

			require.NotEmpty(t, strings.TrimSpace(partial))
			assert.Contains(t, page, strings.TrimSpace(partial),
				"the page's embedded %s section must be the partial htmx serves", tt.section)
		})
	}
}

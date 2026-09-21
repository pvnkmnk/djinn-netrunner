package api

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
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

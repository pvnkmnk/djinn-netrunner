package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Registration still hardcodes the user role. The bootstrap is what makes an
// operator reachable, and the ordering that matters is the one an operator
// actually hits: set BOOTSTRAP_ADMIN_EMAIL, restart, then register.
func TestRegister_PromotesTheBootstrapAccount(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com").Register)

	resp := registerBootstrapUser(t, app, "operator@example.com")
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&u).Error)
	assert.Equal(t, services.AdminRole, u.Role,
		"an operator who configured the address before registering must become admin")
}

// Registering as somebody else must never be escalated, whatever the bootstrap
// is configured to.
func TestRegister_DoesNotPromoteAnyoneElse(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com").Register)

	for _, email := range []string{
		"other@example.com",
		"operator@example.com.evil.test",
		"operator@example.co",
	} {
		resp := registerBootstrapUser(t, app, email)
		require.Equal(t, 201, resp.StatusCode)

		var u database.User
		require.NoError(t, db.Where("email = ?", email).First(&u).Error)
		assert.Equal(t, "user", u.Role, "%s must stay an ordinary user", email)
	}
}

// With no bootstrap configured, registration is unchanged.
func TestRegister_WithoutBootstrapStaysUser(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandler(db).Register)

	resp := registerBootstrapUser(t, app, "someone@example.com")
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "someone@example.com").First(&u).Error)
	assert.Equal(t, "user", u.Role)
}

// The registration path must not double the audit trail. An earlier boot with
// the same address configured found no account, and registering is the moment
// the account appears, so exactly one promotion may be recorded.
func TestRegister_PromotesOnceAfterAFailedBootAttempt(t *testing.T) {
	db := setupTestDBForAuth(t)

	pending, err := services.BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	require.True(t, pending.NoAccount)

	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com").Register)

	resp := registerBootstrapUser(t, app, "operator@example.com")
	require.Equal(t, 201, resp.StatusCode)

	var count int64
	require.NoError(t, db.Model(&database.AuditLog{}).
		Where("action = ?", services.BootstrapAdminAction).
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "registration must complete the pending bootstrap exactly once")
}

// registerBootstrapUser posts a registration through the real route and
// returns the response.
func registerBootstrapUser(t *testing.T, app *fiber.App, email string) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]string{"email": email, "password": "correct horse battery"})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)

	return resp
}

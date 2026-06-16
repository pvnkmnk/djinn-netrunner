package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAdminTestDB creates an in-memory SQLite database for admin auth tests
func setupAdminTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	return db
}

// setupAdminTestApp creates a Fiber app with admin routes and an auth middleware
// that injects the given user into Locals.
func setupAdminTestApp(t *testing.T, db *gorm.DB, user database.User) *fiber.App {
	app := fiber.New()
	handler := NewAdminHandler(db)

	// Inject user into Locals for all routes (simulate auth middleware)
	injectUser := func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}

	// Admin routes with AdminOnly middleware
	registerAdminRoutes(app, handler, injectUser)
	return app
}

// setupAdminTestAppNoAuth creates a Fiber app with admin routes but NO auth middleware.
func setupAdminTestAppNoAuth(t *testing.T, db *gorm.DB) *fiber.App {
	app := fiber.New()
	handler := NewAdminHandler(db)

	// No-op middleware (doesn't inject user)
	noop := func(c *fiber.Ctx) error { return c.Next() }

	registerAdminRoutes(app, handler, noop)
	return app
}

func registerAdminRoutes(app *fiber.App, handler *AdminHandler, authMW fiber.Handler) {
	app.Get("/api/admin/users", authMW, handler.AdminOnly, handler.ListUsers)
	app.Post("/api/admin/users", authMW, handler.AdminOnly, handler.CreateUser)
	app.Delete("/api/admin/users/:id", authMW, handler.AdminOnly, handler.DeleteUser)
	app.Patch("/api/admin/users/:id/role", authMW, handler.AdminOnly, handler.UpdateRole)
	app.Post("/api/admin/users/:id/reset-password", authMW, handler.AdminOnly, handler.ResetPassword)
	app.Get("/api/admin/audit", authMW, handler.AdminOnly, handler.ListAudit)
	app.Get("/api/admin/config", authMW, handler.AdminOnly, handler.ListConfig)
	app.Patch("/api/admin/config", authMW, handler.AdminOnly, handler.UpdateConfig)

	// Partial routes
	app.Get("/partials/admin/users", authMW, handler.AdminOnly, handler.RenderUsersPartial)
	app.Get("/partials/admin/audit", authMW, handler.AdminOnly, handler.RenderAuditPartial)
	app.Get("/partials/admin/config", authMW, handler.AdminOnly, handler.RenderConfigPartial)
}

func TestAdminAuth_Unauthenticated(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create app without any auth middleware — no user in Locals
	app := setupAdminTestAppNoAuth(t, db)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/admin/users", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "not authenticated", result["error"])
}

func TestAdminAuth_NonAdmin(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create a non-admin user
	nonAdminUser := database.User{
		Email:        "test@example.com",
		PasswordHash: "hashed_password",
		Role:         "user",
	}
	require.NoError(t, db.Create(&nonAdminUser).Error)

	// Create app with non-admin user
	app := setupAdminTestApp(t, db, nonAdminUser)

	// Test admin route with non-admin user
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/admin/users", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "forbidden: admin only", result["error"])
}

func TestAdminAuth_AdminAccess(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create an admin user
	adminUser := database.User{
		Email:        "admin@example.com",
		PasswordHash: "hashed_password",
		Role:         "admin",
	}
	require.NoError(t, db.Create(&adminUser).Error)

	// Create app with admin user
	app := setupAdminTestApp(t, db, adminUser)

	// Test admin route with admin user - should succeed
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/admin/users", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Test other admin routes
	req = httptest.NewRequestWithContext(context.Background(), "GET", "/api/admin/audit", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	req = httptest.NewRequestWithContext(context.Background(), "GET", "/api/admin/config", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAdminAuth_CreateUser_Admin(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create an admin user
	adminUser := database.User{
		Email:        "admin@example.com",
		PasswordHash: "hashed_password",
		Role:         "admin",
	}
	require.NoError(t, db.Create(&adminUser).Error)

	// Create app with admin user
	app := setupAdminTestApp(t, db, adminUser)

	// Test creating a new user with admin privileges
	payload := map[string]string{
		"email":    "newuser@example.com",
		"password": "password123",
		"role":     "user",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/admin/users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "created", result["status"])
	assert.NotNil(t, result["id"])
}

func TestAdminAuth_CreateUser_NonAdmin(t *testing.T) {
	db := setupAdminTestDB(t)

	// Create a non-admin user
	nonAdminUser := database.User{
		Email:        "test@example.com",
		PasswordHash: "hashed_password",
		Role:         "user",
	}
	require.NoError(t, db.Create(&nonAdminUser).Error)

	// Create app with non-admin user
	app := setupAdminTestApp(t, db, nonAdminUser)

	// Test creating a new user with non-admin privileges - should be forbidden
	payload := map[string]string{
		"email":    "newuser@example.com",
		"password": "password123",
		"role":     "user",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/admin/users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "forbidden: admin only", result["error"])
}

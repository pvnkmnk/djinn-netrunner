package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProfileSecurityTestApp(t *testing.T, user database.User) (*fiber.App, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	require.NoError(t, db.Create(&user).Error)

	handler := NewProfileHandler(db)
	app := fiber.New()

	injectUser := func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}

	app.Post("/api/profiles", injectUser, handler.Create)
	app.Patch("/api/profiles/:id", injectUser, handler.Update)

	return app, db
}

func TestProfileCreate_PrivilegeEscalation(t *testing.T) {
	user := database.User{
		ID:    1,
		Email: "user@example.com",
		Role:  "user",
	}
	app, db := setupProfileSecurityTestApp(t, user)

	// Attempt to create a profile with IsDefault: true as a non-admin user
	payload := map[string]interface{}{
		"name":       "Malicious Default Profile",
		"is_default": true,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)

	// Verify the fix: status should be 403 Forbidden
	assert.Equal(t, 403, resp.StatusCode)

	var profile database.QualityProfile
	err = db.Where("name = ?", "Malicious Default Profile").First(&profile).Error
	// The record should not even be created if the transaction/check is early,
	// or at least it shouldn't be default if we only blocked the default part.
	// In my implementation, I return 403 early.
	assert.Error(t, err, "Profile should not have been created with invalid permissions")
}

func TestProfileUpdate_PrivilegeEscalation(t *testing.T) {
	user := database.User{
		ID:    1,
		Email: "user@example.com",
		Role:  "user",
	}
	app, db := setupProfileSecurityTestApp(t, user)

	// Create a profile owned by the user
	profileID := uuid.New()
	profile := database.QualityProfile{
		ID:          profileID,
		Name:        "User Profile",
		OwnerUserID: &user.ID,
		IsDefault:   false,
	}
	require.NoError(t, db.Create(&profile).Error)

	// Attempt to update the profile to IsDefault: true as a non-admin user
	payload := map[string]interface{}{
		"is_default": true,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PATCH", "/api/profiles/"+profileID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)

	// Verify the fix: status should be 403 Forbidden
	assert.Equal(t, 403, resp.StatusCode)

	var updatedProfile database.QualityProfile
	err = db.First(&updatedProfile, profileID).Error
	require.NoError(t, err)

	// Verify that the profile IS NOT default
	assert.False(t, updatedProfile.IsDefault, "VULNERABILITY: Non-admin user was able to set IsDefault: true during update")
}

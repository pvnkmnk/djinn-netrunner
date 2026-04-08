package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestProfileSecurity(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	profileHandler := NewProfileHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Post("/api/profiles", profileHandler.Create)
	app.Patch("/api/profiles/:id", profileHandler.Update)

	// Setup non-admin user
	userSecurity := database.User{Email: "security-user@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&userSecurity)

	// Setup session
	sessSecurity := database.Session{SessionID: "sess-security", UserID: userSecurity.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	db.Create(&sessSecurity)

	t.Run("Non-admin cannot create default profile", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":       "Malicious Default",
			"is_default": true,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/profiles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-security"})

		resp, _ := app.Test(req)

		// Currently this is expected to FAIL (returns 201) before the fix
		// After the fix, it should return 403
		assert.Equal(t, 403, resp.StatusCode, "Non-admin should not be allowed to create a default profile")
	})

	t.Run("Non-admin cannot update profile to default", func(t *testing.T) {
		// Create a normal profile first
		profile := database.QualityProfile{
			Name:        "Normal Profile Security",
			IsDefault:   false,
			OwnerUserID: &userSecurity.ID,
		}
		db.Create(&profile)

		payload := map[string]interface{}{
			"is_default": true,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("PATCH", "/api/profiles/"+profile.ID.String(), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-security"})

		resp, _ := app.Test(req)

		// Currently this is expected to FAIL (returns 200) before the fix
		// After the fix, it should return 403
		assert.Equal(t, 403, resp.StatusCode, "Non-admin should not be allowed to update a profile to default")
	})
}

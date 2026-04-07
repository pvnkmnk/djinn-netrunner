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

func TestProfilePrivilegeEscalation(t *testing.T) {
	db := setupTestDB(t)
	// Clean up database tables for this test
	db.Exec("DELETE FROM sessions")
	db.Exec("DELETE FROM users")
	db.Exec("DELETE FROM quality_profiles")

	app := fiber.New()
	auth := NewAuthHandler(db)
	profileHandler := NewProfileHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Post("/api/profiles", profileHandler.Create)
	app.Patch("/api/profiles/:id", profileHandler.Update)

	// Setup non-admin user
	user := database.User{Email: "user@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user)

	// Setup session
	sess := database.Session{SessionID: "sess-user", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	db.Create(&sess)

	// 1. Try to CREATE a default profile as non-admin
	createPayload := map[string]interface{}{
		"name":       "Evil Default",
		"is_default": true,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-user"})
	resp, _ := app.Test(req)

	// ✅ Fixed state: should return 403
	assert.Equal(t, 403, resp.StatusCode, "Non-admin should be Forbidden from creating a default profile")

	// 2. Try to UPDATE a profile to be default as non-admin
	// First create a normal profile
	p := database.QualityProfile{Name: "My Profile", OwnerUserID: &user.ID, IsDefault: false}
	db.Create(&p)

	updatePayload := map[string]interface{}{
		"is_default": true,
	}
	body, _ = json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/profiles/"+p.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-user"})
	resp, _ = app.Test(req)

	assert.Equal(t, 403, resp.StatusCode, "Non-admin should be Forbidden from promoting a profile to default")

	// Verify DB state
	var updatedP database.QualityProfile
	db.First(&updatedP, "id = ?", p.ID)
	assert.False(t, updatedP.IsDefault, "Profile should NOT be default in database")
}

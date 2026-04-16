package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestProfilePrivilegeEscalation(t *testing.T) {
	// 1. Setup DB
	os.Setenv("DATABASE_URL", "profiles_security.db")
	defer os.Remove("profiles_security.db")
	db := setupTestDB(t)

	app := fiber.New()
	auth := NewAuthHandler(db)
	profile := NewProfileHandler(db)

	app.Post("/api/profiles", auth.AuthMiddleware, profile.Create)
	app.Patch("/api/profiles/:id", auth.AuthMiddleware, profile.Update)

	// 2. Create Users
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)

	adminUser := database.User{Email: "admin@example.com", PasswordHash: string(passwordHash), Role: "admin"}
	db.Create(&adminUser)

	regularUser := database.User{Email: "user@example.com", PasswordHash: string(passwordHash), Role: "user"}
	db.Create(&regularUser)

	// 3. Create a session for regular user
	sessionID := "test-session-user"
	db.Create(&database.Session{
		SessionID: sessionID,
		UserID:    regularUser.ID,
		ExpiresAt: time.Now().Add(SessionTTL),
	})

	// 4. Test: Regular user attempts to CREATE a default profile
	createPayload := map[string]interface{}{
		"name":       "Evil Default",
		"is_default": true,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sessionID})

	resp, _ := app.Test(req)
	// Currently it succeeds (returns 201), but we want it to fail (403)
	// For now, let's just assert the current behavior to confirm it's vulnerable,
	// then we'll change the assertion after fixing it.
	// Actually, Sentinel should ideally write a failing test first.
	assert.Equal(t, 403, resp.StatusCode, "Non-admin should not be able to create a default profile")

	// 5. Test: Regular user attempts to UPDATE their profile to be default
	// First, create a normal profile for the user
	userProfile := database.QualityProfile{
		Name:        "User Profile",
		OwnerUserID: &regularUser.ID,
		IsDefault:   false,
	}
	db.Create(&userProfile)

	updatePayload := map[string]interface{}{
		"is_default": true,
	}
	body, _ = json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", fmt.Sprintf("/api/profiles/%s", userProfile.ID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sessionID})

	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode, "Non-admin should not be able to promote their profile to default")
}

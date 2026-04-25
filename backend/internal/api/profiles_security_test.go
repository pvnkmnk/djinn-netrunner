package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestProfilePrivilegeEscalation(t *testing.T) {
	db := setupTestDBForAuth(t)

	// Use unique test ID for isolation
	testID := uuid.New().String()

	app := fiber.New()
	auth := NewAuthHandler(db)
	profileHandler := NewProfileHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Post("/api/profiles", profileHandler.Create)
	app.Patch("/api/profiles/:id", profileHandler.Update)

	// Setup non-admin user
	user := database.User{Email: "user-" + testID + "@example.com", PasswordHash: "hash", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Setup session
	sess := database.Session{SessionID: "sess-" + testID, UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Cleanup function
	defer func() {
		db.Exec("DELETE FROM sessions WHERE session_id LIKE ?", "%"+testID)
		db.Exec("DELETE FROM quality_profiles WHERE name LIKE ?", "%"+testID)
		db.Exec("DELETE FROM users WHERE id = ?", user.ID)
	}()

	// 1. Try to CREATE a default profile as non-admin
	createPayload := map[string]interface{}{
		"name":       "Evil Default-" + testID,
		"is_default": true,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess.SessionID})
	resp, _ := app.Test(req)

	// ✅ Fixed state: should return 403
	assert.Equal(t, 403, resp.StatusCode, "Non-admin should be Forbidden from creating a default profile")

	// 2. Try to UPDATE a profile to be default as non-admin
	// First create a normal profile
	p := database.QualityProfile{Name: "My Profile-" + testID, OwnerUserID: &user.ID, IsDefault: false}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("Failed to create test profile: %v", err)
	}
	
	// Verify profile was created
	var checkP database.QualityProfile
	if err := db.First(&checkP, "id = ?", p.ID).Error; err != nil {
		t.Fatalf("Profile not found after creation: %v (profile ID: %s, owner_user_id: %d)", err, p.ID, user.ID)
	}
	if checkP.OwnerUserID != nil && *checkP.OwnerUserID != user.ID {
		t.Fatalf("Profile owner mismatch: expected %d, got %v", user.ID, checkP.OwnerUserID)
	}

	updatePayload := map[string]interface{}{
		"is_default": true,
	}
	body, _ = json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/profiles/"+p.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: sess.SessionID})
	resp, _ = app.Test(req)

	assert.Equal(t, 403, resp.StatusCode, "Non-admin should be Forbidden from promoting a profile to default")

	// Verify DB state
	var updatedP database.QualityProfile
	db.First(&updatedP, "id = ?", p.ID)
	assert.False(t, updatedP.IsDefault, "Profile should NOT be default in database")
}

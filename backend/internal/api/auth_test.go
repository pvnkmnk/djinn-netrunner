package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// setupTestDBForAuth creates an in-memory SQLite database for complete test isolation
func setupTestDBForAuth(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	database.Migrate(db)
	return db
}

func TestRegister_ReturnsCreatedWithoutSessionCookie(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandler(db).Register)

	body, err := json.Marshal(map[string]string{
		"email":    "register-no-session@example.com",
		"password": "correct-horse-battery-staple",
	})
	if !assert.NoError(t, err) {
		return
	}

	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if !assert.NoError(t, err) {
		return
	}
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Empty(t, resp.Header.Values("Set-Cookie"))

	decoder := json.NewDecoder(resp.Body)
	var response map[string]string
	if !assert.NoError(t, decoder.Decode(&response)) {
		return
	}
	assert.Equal(t, map[string]string{"status": "ok"}, response)

	var trailing any
	assert.ErrorIs(t, decoder.Decode(&trailing), io.EOF)
}

func TestAuthFlow(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	auth := NewAuthHandler(db)

	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)
	app.Get("/protected", auth.AuthMiddleware, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Use UUID-based email for test isolation
	email := "test-" + uuid.New().String() + "@example.com"
	password := "correct-horse-battery-staple"

	// 1. Register
	regPayload := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ := json.Marshal(regPayload)
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)

	// 2. Login
	loginPayload := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ = json.Marshal(loginPayload)
	req = httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)

	// Extract cookie
	var cookieStr string
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			cookieStr = c.Value
		}
	}
	assert.NotEmpty(t, cookieStr)

	// 3. Protected
	req = httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookieStr})
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

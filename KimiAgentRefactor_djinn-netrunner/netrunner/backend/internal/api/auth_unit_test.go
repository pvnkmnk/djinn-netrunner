package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupInMemoryDB creates an in-memory SQLite DB for testing.
func setupInMemoryDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	database.Migrate(db)
	return db
}

func TestRegister_NewUser(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)

	body, _ := json.Marshal(map[string]string{
		"email":    "new@example.com",
		"password": "password123",
	})
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["status"])
}

func TestRegister_ExistingUser_NoEnumeration(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)

	body, _ := json.Marshal(map[string]string{
		"email":    "existing@example.com",
		"password": "password123",
	})

	// First registration
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp1, _ := app.Test(req)

	// Second registration (duplicate)
	req = httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req)

	// Both should return identical status and body
	assert.Equal(t, resp1.StatusCode, resp2.StatusCode)

	var result1, result2 map[string]string
	json.NewDecoder(resp1.Body).Decode(&result1)
	json.NewDecoder(resp2.Body).Decode(&result2)
	assert.Equal(t, result1, result2, "duplicate registration must return identical response")
}

func TestRegister_MissingFields(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"empty email", map[string]string{"password": "pass"}},
		{"empty password", map[string]string{"email": "a@b.com"}},
		{"both empty", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)
			assert.Equal(t, 400, resp.StatusCode)
		})
	}
}

func TestLogin_Success(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)

	// Register first
	body, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "pass123"})
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	regResp, _ := app.Test(req)
	require.Equal(t, 201, regResp.StatusCode)

	// Login
	body, _ = json.Marshal(map[string]string{"email": "user@example.com", "password": "pass123"})
	req = httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 302, resp.StatusCode)

	// Verify session cookie is set
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			found = true
			assert.NotEmpty(t, c.Value)
			assert.True(t, c.HttpOnly)
		}
	}
	assert.True(t, found, "session cookie should be set")
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)

	body, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "correct"})
	app.Test(httptest.NewRequest("POST", "/register", bytes.NewBuffer(body)))

	body, _ = json.Marshal(map[string]string{"email": "user@example.com", "password": "wrong"})
	req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 401, resp.StatusCode)
}

func TestLogin_NonexistentUser(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/login", auth.Login)

	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com", "password": "pass"})
	req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 401, resp.StatusCode)
}

func TestLogout(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)
	app.Post("/logout", auth.Logout)

	// Register
	regBody, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "pass"})
	regReq := httptest.NewRequest("POST", "/register", bytes.NewBuffer(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regResp, _ := app.Test(regReq)
	require.Equal(t, 201, regResp.StatusCode)

	// Login
	loginBody, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "pass"})
	loginReq := httptest.NewRequest("POST", "/login", bytes.NewBuffer(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, _ := app.Test(loginReq)
	require.Equal(t, 302, loginResp.StatusCode)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Cookies() {
		if c.Name == SessionCookie {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie)

	// Logout
	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(sessionCookie)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Verify session is deleted from DB
	var count int64
	db.Model(&database.Session{}).Where("session_id = ?", sessionCookie.Value).Count(&count)
	assert.Equal(t, int64(0), count, "session should be deleted after logout")
}

func TestAuthMiddleware_NoCookie(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Get("/protected", auth.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestAuthMiddleware_InvalidSession(t *testing.T) {
	db := setupInMemoryDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Get("/protected", auth.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "invalid-session-id"})
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

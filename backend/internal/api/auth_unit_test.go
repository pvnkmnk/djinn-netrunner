package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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
		"password": "correct-horse-battery-staple",
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
		"password": "correct-horse-battery-staple",
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
	body, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "correct-horse-battery-staple"})
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	regResp, _ := app.Test(req)
	require.Equal(t, 201, regResp.StatusCode)

	// Login
	body, _ = json.Marshal(map[string]string{"email": "user@example.com", "password": "correct-horse-battery-staple"})
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
	regBody, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "correct-horse-battery-staple"})
	regReq := httptest.NewRequest("POST", "/register", bytes.NewBuffer(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regResp, _ := app.Test(regReq)
	require.Equal(t, 201, regResp.StatusCode)

	// Login
	loginBody, _ := json.Marshal(map[string]string{"email": "user@example.com", "password": "correct-horse-battery-staple"})
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
	app.Get("/protected", auth.AuthMiddleware, func(c fiber.Ctx) error {
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
	app.Get("/protected", auth.AuthMiddleware, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "invalid-session-id"})
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestAuthMiddleware_DisabledUserRejected(t *testing.T) {
	db := setupInMemoryDB(t)
	disabledAt := time.Now()
	user := database.User{Email: "disabled@example.com", PasswordHash: "hash", Role: "user", DisabledAt: &disabledAt}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&database.Session{
		SessionID: "disabled-session",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error)

	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Get("/protected", auth.AuthMiddleware, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "disabled-session"})
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestOptionalAuthMiddleware_DisabledUserNotLoaded(t *testing.T) {
	db := setupInMemoryDB(t)
	disabledAt := time.Now()
	user := database.User{Email: "optional-disabled@example.com", PasswordHash: "hash", Role: "user", DisabledAt: &disabledAt}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&database.Session{
		SessionID: "optional-disabled-session",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error)

	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Get("/optional", auth.OptionalAuthMiddleware, func(c fiber.Ctx) error {
		if _, ok := c.Locals("user").(database.User); ok {
			return c.SendString("loaded")
		}
		return c.SendString("anonymous")
	})

	req := httptest.NewRequest("GET", "/optional", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "optional-disabled-session"})
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "anonymous", string(body))
}

func TestLogin_DisabledUserRejected(t *testing.T) {
	db := setupInMemoryDB(t)
	auth := NewAuthHandler(db)
	app := fiber.New()
	app.Post("/login", auth.Login)

	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery-staple"), BcryptCost)
	require.NoError(t, err)
	disabledAt := time.Now()
	require.NoError(t, db.Create(&database.User{
		Email:        "login-disabled@example.com",
		PasswordHash: string(hash),
		Role:         "user",
		DisabledAt:   &disabledAt,
	}).Error)

	body := bytes.NewBufferString(`{"email":"login-disabled@example.com","password":"correct-horse-battery-staple"}`)
	req := httptest.NewRequest("POST", "/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestLogout_RevokesOnlyPresentedSession(t *testing.T) {
	db := setupInMemoryDB(t)
	auth := NewAuthHandler(db)
	app := fiber.New()
	app.Post("/logout", auth.Logout)

	user := database.User{Email: "logout-sessions@example.com", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	first := database.Session{SessionID: "logout-first", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	second := database.Session{SessionID: "logout-second", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: first.SessionID})
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var remaining int64
	db.Model(&database.Session{}).Where("session_id = ?", first.SessionID).Count(&remaining)
	assert.Equal(t, int64(0), remaining)
	db.Model(&database.Session{}).Where("session_id = ?", second.SessionID).Count(&remaining)
	assert.Equal(t, int64(1), remaining)
}

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	var dialector gorm.Dialector
	if strings.HasPrefix(dbURL, "postgres") {
		dialector = postgres.Open(dbURL)
	} else {
		dialector = sqlite.Open(dbURL)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	database.Migrate(db)
	return db
}

func TestAuthFlow(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)

	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)
	app.Get("/protected", auth.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	email := "test@example.com"
	password := "password123"

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
	assert.Equal(t, 200, resp.StatusCode)

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

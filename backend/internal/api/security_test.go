package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestRegisterPrivilegeEscalation(t *testing.T) {
	// Set DATABASE_URL for setupTestDB to use SQLite in-memory
	os.Setenv("DATABASE_URL", ":memory:")
	defer os.Unsetenv("DATABASE_URL")

	db := setupTestDB(t)
	app := fiber.New()
	auth := NewAuthHandler(db)

	app.Post("/register", auth.Register)

	email := "admin_attempt@example.com"
	password := "password123"

	// Attempt to register with 'admin' role
	regPayload := map[string]string{
		"email":    email,
		"password": password,
		"role":     "admin",
	}
	body, _ := json.Marshal(regPayload)
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	// Verify the user's role in the database
	var user database.User
	err := db.Where("email = ?", email).First(&user).Error
	assert.NoError(t, err)

	// Verify that the role is 'user' despite the 'admin' request
	assert.Equal(t, "user", user.Role, "User should have been assigned 'user' role regardless of input")
}

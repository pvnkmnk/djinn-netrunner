package api

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// Token authentication with no configured SUBSONIC_PASSWORD must be refused.
//
// md5Password is only populated when a password is configured (see
// NewSubsonicHandler); without it the expected token degenerates to
// md5("" + salt) — a value any caller can compute from the salt it just sent.
// Enabling Subsonic without a password must not silently accept that forgery.
// Account-password (p=) authentication is deliberately unaffected.
func TestSubsonic_AuthMiddleware_TokenRejectedWhenNoPasswordConfigured(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: "",
		},
	}
	handler := NewSubsonicHandler(db, cfg)
	createTestUserForSubsonic(t, db, "nopass@example.com", "accountpass")

	// The exact forgery an attacker can build when no shared password is
	// configured: md5Password is "", so the expected token is md5("" + salt).
	// Sending anything else would not prove the guard is what rejects us.
	salt := "attsalt"
	hash := md5.Sum([]byte(salt))
	token := hex.EncodeToString(hash[:])

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/test?u=nopass@example.com&t=%s&s=%s&v=1.16.1&c=test", token, salt), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	// Subsonic reports auth failures inside a 200 envelope with code 40.
	assert.Equal(t, 200, resp.StatusCode)
	bodyStr := string(subsonicGetRespBody(resp))
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.NotContains(t, bodyStr, "OK")
}

// And the account-password path still works with no shared password set, so
// enabling Subsonic without SUBSONIC_PASSWORD degrades instead of bricking.
func TestSubsonic_AuthMiddleware_PasswordAuthStillWorksWhenNoSharedPassword(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: "",
		},
	}
	handler := NewSubsonicHandler(db, cfg)
	createTestUserForSubsonic(t, db, "pwonly@example.com", "accountpass")

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test?u=pwonly@example.com&p=accountpass", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "OK", string(subsonicGetRespBody(resp)))
}

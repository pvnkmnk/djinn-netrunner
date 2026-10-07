package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestGenerateOAuthState_Random(t *testing.T) {
	// Generate multiple states and verify they are unique
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		state, err := generateOAuthState()
		if err != nil {
			t.Fatalf("generateOAuthState() returned error: %v", err)
		}
		if len(state) != 64 {
			t.Errorf("expected state length 64 (32 bytes hex-encoded), got %d", len(state))
		}
		if seen[state] {
			t.Errorf("duplicate state generated: %s", state)
		}
		seen[state] = true
	}
}

func TestGenerateOAuthState_Format(t *testing.T) {
	state, err := generateOAuthState()
	if err != nil {
		t.Fatalf("generateOAuthState() returned error: %v", err)
	}

	// Verify it's valid hex
	if len(state) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(state))
	}

	for _, c := range state {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character: %c", c)
		}
	}
}

func TestSpotifyAuthHandler_LoginCookieSecure(t *testing.T) {
	db := setupTestDBForAuth(t)
	handler := NewSpotifyAuthHandler(db)

	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		ProxyHeader:             fiber.HeaderXForwardedProto,
		TrustedProxies:          []string{"127.0.0.1", "0.0.0.0/0"},
	})
	app.Get("/auth/spotify/login", handler.Login)

	// Test HTTP request -> Secure cookie should NOT be set
	reqHTTP := httptest.NewRequest("GET", "http://example.com/auth/spotify/login", nil)
	respHTTP, err := app.Test(reqHTTP, -1)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusFound, respHTTP.StatusCode)

	cookiesHTTP := respHTTP.Cookies()
	var stateCookieHTTP string
	for _, cookie := range cookiesHTTP {
		if cookie.Name == oauthStateCookie {
			stateCookieHTTP = cookie.Raw
			assert.False(t, cookie.Secure, "HTTP request should not produce secure cookie")
			assert.True(t, cookie.HttpOnly)
		}
	}
	assert.NotEmpty(t, stateCookieHTTP)

	// Test HTTPS request -> Secure cookie SHOULD be set
	reqHTTPS := httptest.NewRequest("GET", "http://example.com/auth/spotify/login", nil)
	reqHTTPS.Header.Set("X-Forwarded-Proto", "https")
	respHTTPS, err := app.Test(reqHTTPS, -1)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusFound, respHTTPS.StatusCode)

	cookiesHTTPS := respHTTPS.Cookies()
	var stateCookieHTTPS string
	for _, cookie := range cookiesHTTPS {
		if cookie.Name == oauthStateCookie {
			stateCookieHTTPS = cookie.Raw
			assert.True(t, cookie.Secure, "HTTPS request should produce secure cookie")
			assert.True(t, cookie.HttpOnly)
			assert.Contains(t, strings.ToLower(cookie.Raw), "secure")
		}
	}
	assert.NotEmpty(t, stateCookieHTTPS)
}

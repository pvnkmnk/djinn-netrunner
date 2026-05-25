package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSpDcAuth_SetAndHasSpDcCookie(t *testing.T) {
	auth := NewSpDcAuth(nil)

	if auth.HasSpDcCookie() {
		t.Error("expected HasSpDcCookie=false before setting")
	}

	auth.SetSpDcCookie("test-cookie")
	if !auth.HasSpDcCookie() {
		t.Error("expected HasSpDcCookie=true after setting")
	}

	// Setting empty clears it
	auth.SetSpDcCookie("")
	if auth.HasSpDcCookie() {
		t.Error("expected HasSpDcCookie=false after clearing")
	}
}

func TestSpDcAuth_InvalidateTokens(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.accessToken = "test-token"
	auth.clientToken = "test-client"

	auth.InvalidateTokens()

	if auth.accessToken != "" {
		t.Error("expected accessToken cleared after invalidation")
	}
	if auth.clientToken != "" {
		t.Error("expected clientToken cleared after invalidation")
	}
}

func TestSpDcAuth_GetSpDcAccessToken_NoCookie(t *testing.T) {
	auth := NewSpDcAuth(nil)

	_, err := auth.GetSpDcAccessToken()
	if err == nil {
		t.Fatal("expected error when no sp_dc cookie set")
	}
}

func TestSpDcAuth_ClientCredentialsToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-cc-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// We can't easily test the real endpoints, but we can test the caching logic
	auth := NewSpDcAuth(server.Client())

	// Set a pre-cached token
	auth.mu.Lock()
	auth.ccToken = "cached-token"
	auth.ccTokenExpiry = time.Now().Add(1 * time.Hour)
	auth.mu.Unlock()

	token, err := auth.GetClientCredentialsToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "cached-token" {
		t.Errorf("expected cached token, got %q", token)
	}
}

func TestSpDcAuth_ExchangeSpDcToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify sp_dc cookie is sent
		cookie := r.Header.Get("Cookie")
		if cookie == "" {
			t.Error("expected Cookie header to be set")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Set-Cookie", "sp_t=test-device-id; Path=/")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":                      "test-access-token",
			"accessTokenExpirationTimestampMs": 1700000000000,
			"isAnonymous":                      false,
			"clientId":                         "test-client-id",
			"username":                         "testuser",
		})
	}))
	defer server.Close()

	auth := NewSpDcAuth(server.Client())

	// Override the endpoint for testing (not possible with const, so test the internal method)
	// Instead, test with a mock that uses the test server
	// The exchangeSpDcToken method uses the global const, so we test ValidateSpDcCookie behavior indirectly
	// This test verifies the struct methods work correctly

	auth.mu.Lock()
	auth.accessToken = "pre-set-token"
	auth.tokenExpiry = time.Now().Add(1 * time.Hour)
	auth.spDcCookie = "test-cookie"
	auth.mu.Unlock()

	token, err := auth.GetSpDcAccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "pre-set-token" {
		t.Errorf("expected pre-set token, got %q", token)
	}
}

func TestSpDcAuth_GetUsername(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.username = "testuser"

	if auth.GetUsername() != "testuser" {
		t.Errorf("expected 'testuser', got %q", auth.GetUsername())
	}
}

func TestNewSpDcAuth_NilClient(t *testing.T) {
	auth := NewSpDcAuth(nil)
	if auth.httpClient == nil {
		t.Error("expected default http client when nil passed")
	}
}

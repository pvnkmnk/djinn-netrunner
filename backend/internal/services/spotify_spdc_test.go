package services

import (
	"testing"
	"time"
)

func TestSpDcAuth_SetAndHasSpDcCookie(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.SetActiveUser(1)

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

func TestSpDcAuth_PerUserSessionIsolation(t *testing.T) {
	auth := NewSpDcAuth(nil)

	// Set up user 1
	auth.SetActiveUser(1)
	auth.SetSpDcCookie("user1-cookie")
	if !auth.HasSpDcCookie() {
		t.Fatal("expected user 1 to have cookie")
	}

	// Set up user 2
	auth.SetActiveUser(2)
	auth.SetSpDcCookie("user2-cookie")
	if !auth.HasSpDcCookie() {
		t.Fatal("expected user 2 to have cookie")
	}

	// Switch back to user 1 — their cookie should still be set
	auth.SetActiveUser(1)
	if !auth.HasSpDcCookie() {
		t.Fatal("expected user 1 cookie to persist after switching users")
	}

	// User 3 has no cookie
	auth.SetActiveUser(3)
	if auth.HasSpDcCookie() {
		t.Error("expected user 3 to have no cookie")
	}
}

func TestSpDcAuth_InvalidateTokens(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.SetActiveUser(1)

	auth.mu.Lock()
	sess := auth.getOrCreateSession()
	sess.accessToken = "test-token"
	sess.clientToken = "test-client"
	auth.mu.Unlock()

	auth.InvalidateTokens()

	auth.mu.RLock()
	s := auth.getSession()
	auth.mu.RUnlock()

	if s.accessToken != "" {
		t.Error("expected accessToken cleared after invalidation")
	}
	if s.clientToken != "" {
		t.Error("expected clientToken cleared after invalidation")
	}
}

func TestSpDcAuth_GetSpDcAccessToken_NoCookie(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.SetActiveUser(1)

	_, err := auth.GetSpDcAccessToken()
	if err == nil {
		t.Fatal("expected error when no sp_dc cookie set")
	}
}

func TestSpDcAuth_CachedAccessToken(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.SetActiveUser(1)

	// Pre-set a cached token
	auth.mu.Lock()
	sess := auth.getOrCreateSession()
	sess.spDcCookie = "test-cookie"
	sess.accessToken = "pre-set-token"
	sess.tokenExpiry = time.Now().Add(1 * time.Hour)
	auth.mu.Unlock()

	token, err := auth.GetSpDcAccessToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "pre-set-token" {
		t.Errorf("expected pre-set token, got %q", token)
	}
}

func TestSpDcAuth_CachedClientCredentials(t *testing.T) {
	auth := NewSpDcAuth(nil)

	// Pre-set a cached client credentials token (shared, not per-user)
	auth.mu.Lock()
	auth.ccToken = "cached-cc-token"
	auth.ccTokenExpiry = time.Now().Add(1 * time.Hour)
	auth.mu.Unlock()

	token, err := auth.GetClientCredentialsToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "cached-cc-token" {
		t.Errorf("expected cached cc token, got %q", token)
	}
}

func TestSpDcAuth_GetUsername(t *testing.T) {
	auth := NewSpDcAuth(nil)
	auth.SetActiveUser(1)

	auth.mu.Lock()
	sess := auth.getOrCreateSession()
	sess.username = "testuser"
	auth.mu.Unlock()

	if auth.GetUsername() != "testuser" {
		t.Errorf("expected 'testuser', got %q", auth.GetUsername())
	}

	// Different user should have no username
	auth.SetActiveUser(2)
	if auth.GetUsername() != "" {
		t.Errorf("expected empty username for user 2, got %q", auth.GetUsername())
	}
}

func TestNewSpDcAuth_NilClient(t *testing.T) {
	auth := NewSpDcAuth(nil)
	if auth.httpClient == nil {
		t.Error("expected default http client when nil passed")
	}
	if auth.userSessions == nil {
		t.Error("expected initialized userSessions map")
	}
}

func TestSpDcAuth_SetSpDcCookie_WithoutActiveUser(t *testing.T) {
	auth := NewSpDcAuth(nil)
	// No SetActiveUser called — activeUserID is 0
	auth.SetSpDcCookie("some-cookie")

	// Should be a no-op (warning logged)
	if auth.HasSpDcCookie() {
		t.Error("expected no cookie when no active user set")
	}
}

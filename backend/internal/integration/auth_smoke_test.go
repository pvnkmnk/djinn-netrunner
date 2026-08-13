//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestSmoke_Auth_RegisterLoginLogout(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Register a new user
	email := "smoke-auth-" + t.Name() + "@test.com"
	body := `{"email":"` + email + `","password":"TestPass123!"}`
	resp, err := client.Post(baseURL+"/api/auth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Register request failed: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Register expected 200/201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Login
	resp, err = client.Post(baseURL+"/api/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Login expected 200, got %d — body: %s", resp.StatusCode, readBody(resp))
	}

	// Verify login response
	var loginResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&loginResp)
	resp.Body.Close()
	if loginResp["status"] != "ok" {
		t.Fatalf("Login response unexpected: %v", loginResp)
	}

	// Verify session cookie was set (cookiejar stores it)
	if len(client.Jar.Cookies(resp.Request.URL)) == 0 {
		t.Fatal("No session cookie set after login")
	}

	// Logout
	resp, err = client.Post(baseURL+"/api/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("Logout request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Logout expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSmoke_Auth_RateLimit(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")

	client := &http.Client{}

	// Hit login rapidly to trigger rate limiter (default: 10 req/min)
	body := `{"email":"ratelimit-test@test.com","password":"wrongpass"}`
	rateLimited := false
	for i := 0; i < 20; i++ {
		resp, err := client.Post(baseURL+"/api/auth/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		if resp.StatusCode == 429 {
			rateLimited = true
			resp.Body.Close()
			break
		}
		resp.Body.Close()
	}
	if !rateLimited {
		t.Error("Expected rate limit (429) after rapid login attempts, never triggered")
	}
}

// readBody reads the full response body and returns it as a string
func readBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	buf := new(strings.Builder)
	io.Copy(buf, resp.Body)
	resp.Body.Close()
	return buf.String()
}
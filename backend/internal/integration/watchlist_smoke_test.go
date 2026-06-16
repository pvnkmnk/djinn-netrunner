//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func TestSmoke_Watchlist_CRUD(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	client := integrationAuthClient(t, baseURL)

	// Create watchlist
	body := `{"name":"smoke-test-wl","source_type":"local","source_uri":"/tmp/smoke-music","quality_profile_id":"00000000-0000-0000-0000-000000000001"}`
	resp, err := client.Post(baseURL+"/api/watchlists/", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Create watchlist failed: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Create watchlist expected 200/201, got %d", resp.StatusCode)
	}

	var created map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	resp.Body.Close()

	// List watchlists
	resp, err = client.Get(baseURL + "/api/watchlists/")
	if err != nil {
		t.Fatalf("List watchlists failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("List watchlists expected 200, got %d", resp.StatusCode)
	}

	// Delete watchlist
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatal("Watchlist creation did not return a valid ID")
	}
	req, _ := http.NewRequest("DELETE", baseURL+"/api/watchlists/"+id, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Delete watchlist failed: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		t.Fatalf("Delete watchlist expected 200/204, got %d", resp.StatusCode)
	}
}

func integrationAuthClient(t *testing.T, baseURL string) *http.Client {
	t.Helper()
	return authClientForTest(t, baseURL, "smoke-"+t.Name())
}

func authClientForTest(t *testing.T, baseURL, emailPrefix string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	email := emailPrefix + "@test.com"
	body := `{"email":"` + email + `","password":"TestPass123!"}`

	resp, err := client.Post(baseURL+"/api/auth/register", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Auth setup register failed: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Auth setup login failed: %v", err)
	}
	resp.Body.Close()

	return client
}
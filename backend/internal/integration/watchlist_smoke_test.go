//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestSmoke_Watchlist_CRUD(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	client := integrationAuthClient(t, baseURL)

	// Create watchlist. Resolve the seeded default quality profile dynamically
	// instead of hardcoding a UUID that doesn't exist in a fresh database.
	// Source type must be one the app registers ("local_file", not "local") and
	// the URI must exist inside the app container (the provider stats it).
	profileID := resolveDefaultQualityProfileID(t)
	sourceURI := "/app/music"
	cleanupWatchlistsAtURI(t, sourceURI)
	t.Cleanup(func() { cleanupWatchlistsAtURI(t, sourceURI) })
	body := `{"name":"smoke-test-wl","source_type":"local_file","source_uri":"` + sourceURI + `","quality_profile_id":"` + profileID + `"}`
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

	// Delete watchlist. The Watchlist model has no json tags, so Go marshals
	// the ID field as "ID" (capitalized); accept both spellings.
	id, ok := created["id"].(string)
	if !ok || id == "" {
		id, ok = created["ID"].(string)
	}
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

// integrationAdminClient returns an authenticated admin session client by
// registering a fresh user and promoting them in the database.
func integrationAdminClient(t *testing.T, baseURL string) *http.Client {
	t.Helper()
	email := "smoke-admin-" + t.Name() + "@test.com"
	client := authClientForTest(t, baseURL, "smoke-admin-"+t.Name())
	dbCfg := &config.Config{DatabaseURL: databaseURL, AllowPrivateTargets: true}
	db, err := database.Connect(dbCfg)
	if err != nil {
		t.Fatalf("Failed to connect to integration database: %v", err)
	}
	promoteUserToAdmin(t, db, email)
	if sql, err := db.DB(); err == nil && sql != nil {
		sql.Close()
	}
	return client
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
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Auth setup register expected 200/201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Auth setup login failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Auth setup login expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	return client
}
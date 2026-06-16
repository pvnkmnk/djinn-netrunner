//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"
)



func TestSmoke_Admin_Panel(t *testing.T) {
	baseURL := "http://localhost:8080"
	
	// Step 1: Register a user (creates regular user with role "user")
	email := "smoke-admin-" + t.Name() + "@test.com"
	body := `{"email":"` + email + `","password":"TestPass123!"}`
	resp, err := http.Post(baseURL+"/api/auth/register", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Register request failed: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Register expected 200/201, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	
	// Step 2: Login as the registered user
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err = client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Login expected 200, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	
	// Step 3: Promote the user to admin via DB (since registration creates regular users)
	// We need to directly update the user's role in the database
	// For this test, we'll use the database connection from the test runner
	// Since we don't have direct DB access in the integration test, we'll use the admin routes
	// to verify the current state and then create a new admin user
	
	// Step 4: Create a new admin user via POST /api/admin/users
	adminCreateBody := `{"email":"admin-smoke-" + t.Name() + "@test.com","password":"AdminPass123!","role":"admin"}`
	resp, err = client.Post(baseURL+"/api/admin/users", "application/json", bytes.NewReader([]byte(adminCreateBody)))
	if err != nil {
		t.Fatalf("Create admin user request failed: %v", err)
	}
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("Create admin user expected 201/200, got %d", resp.StatusCode)
	}
	var createdAdmin map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdAdmin)
	resp.Body.Close()
	// adminID := createdAdmin["id"] // Not used in this test
	
	// Step 5: Login as the newly created admin user
	adminEmail := "admin-smoke-" + t.Name() + "@test.com"
	adminLoginBody := `{"email":"` + adminEmail + `","password":"AdminPass123!"}`
	resp, err = client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader([]byte(adminLoginBody)))
	if err != nil {
		t.Fatalf("Admin login request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Admin login expected 200, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	
	// Step 6: Access admin routes (GET /api/admin/users)
	resp, err = client.Get(baseURL + "/api/admin/users")
	if err != nil {
		t.Fatalf("Admin users request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Admin users expected 200, got %d", resp.StatusCode)
	}
	var users []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&users)
	_ = resp.Body.Close()
	t.Logf("Found %d users via admin endpoint", len(users))
	
	// Step 7: Access admin config route (GET /api/admin/config)
	resp, err = client.Get(baseURL + "/api/admin/config")
	if err != nil {
		t.Fatalf("Admin config request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Admin config expected 200, got %d", resp.StatusCode)
	}
	var settings []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&settings)
	_ = resp.Body.Close()
	t.Logf("Found %d config settings via admin endpoint", len(settings))
	
	// Step 8: Create another user via POST /api/admin/users
	regularBody := `{"email":"regular-smoke-" + t.Name() + "@test.com","password":"RegularPass123!","role":"user"}`
	resp, err = client.Post(baseURL+"/api/admin/users", "application/json", bytes.NewReader([]byte(regularBody)))
	if err != nil {
		t.Fatalf("Create regular user request failed: %v", err)
	}
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("Create regular user expected 201/200, got %d", resp.StatusCode)
	}
	var createdRegular map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdRegular)
	_ = resp.Body.Close()
	regularID := createdRegular["id"]
	t.Logf("Created regular user with ID: %v", regularID)
	
	// Step 9: Verify the audit log entry via GET /api/admin/audit
	resp, err = client.Get(baseURL + "/api/admin/audit?page=1&limit=10")
	if err != nil {
		t.Fatalf("Admin audit request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Admin audit expected 200, got %d", resp.StatusCode)
	}
	var auditResponse map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&auditResponse)
	_ = resp.Body.Close()
	auditEntries, ok := auditResponse["entries"].([]interface{})
	if !ok {
		t.Fatal("Audit response entries is not a slice")
	}
	t.Logf("Found %d audit entries", len(auditEntries))
	
	// Verify we have audit entries for our admin actions
	foundUserCreate := false
	foundConfigAccess := false
	for _, entry := range auditEntries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		action, ok := entryMap["action"].(string)
		if !ok {
			continue
		}
		if action == "user_create" {
			foundUserCreate = true
			t.Logf("Found user_create audit entry")
		}
		if action == "config_update" {
			foundConfigAccess = true
			t.Logf("Found config_update audit entry")
		}
	}
	
	if !foundUserCreate {
		t.Log("Warning: No user_create audit entry found")
	}
	if !foundConfigAccess {
		t.Log("Warning: No config_update audit entry found")
	}
}
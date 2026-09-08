//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestSmoke_Library_CRUD(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	client := integrationAuthClient(t, baseURL)

	// The library path must exist inside the app container (validateLibraryPath
	// stats it there). /app/music is created by the image entrypoint, so it is
	// always valid. Library.Path is unique-indexed, so pre-clean any residue
	// from a previous crashed run and delete our row again at test end.
	libPath := "/app/music"
	cleanupLibrariesAtPath(t, libPath)
	t.Cleanup(func() { cleanupLibrariesAtPath(t, libPath) })
	libName := "smoke-test-lib-" + time.Now().Format("150405")

	// Create library
	body := `{"name":"` + libName + `","path":"` + libPath + `"}`
	resp, err := client.Post(baseURL+"/api/libraries/", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("Create library failed: %v", err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("Create library expected 200/201, got %d", resp.StatusCode)
	}

	var created map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	resp.Body.Close()

	// List libraries
	resp, err = client.Get(baseURL + "/api/libraries/")
	if err != nil {
		t.Fatalf("List libraries failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("List libraries expected 200, got %d", resp.StatusCode)
	}

	// Get library details
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatal("Library creation did not return a valid ID")
	}
	resp, err = client.Get(baseURL + "/api/libraries/" + id)
	if err != nil {
		t.Fatalf("Get library failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Get library expected 200, got %d", resp.StatusCode)
	}
}
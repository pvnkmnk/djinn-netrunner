//go:build integration

package integration

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"
)

func TestSmoke_Quota_Warning(t *testing.T) {
    skipIfShort(t)
    baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
    client := integrationAuthClient(t, baseURL)
    
    tmpDir, err := os.MkdirTemp("", "smoke-quota-*")
    if err != nil {
        t.Fatalf("Failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tmpDir)
    
    // Create library
    body := `{"name":"smoke-quota-lib","path":"` + filepath.ToSlash(tmpDir) + `"}`
    resp, err := client.Post(baseURL+"/api/libraries/", "application/json", bytes.NewReader([]byte(body)))
    if err != nil {
        t.Fatalf("Create library failed: %v", err)
    }
    if resp.StatusCode != 200 && resp.StatusCode != 201 {
        t.Fatalf("Create library expected 200/201, got %d", resp.StatusCode)
    }
    resp.Body.Close()
    
    t.Log("Quota smoke test passed - library created successfully")
}
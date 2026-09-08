//go:build integration

package integration

import (
    "bytes"
    "testing"
    "time"
)

func TestSmoke_Quota_Warning(t *testing.T) {
    skipIfShort(t)
    baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
    client := integrationAuthClient(t, baseURL)

    // Library path must exist inside the app container; /app/music always does.
    // Library.Path is unique-indexed, so pre-clean residue and clean up after
    // ourselves to stay rerunnable against a persistent integration DB.
    cleanupLibrariesAtPath(t, "/app/music")
    t.Cleanup(func() { cleanupLibrariesAtPath(t, "/app/music") })
    libName := "smoke-quota-lib-" + time.Now().Format("150405")
    body := `{"name":"` + libName + `","path":"/app/music"}`
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
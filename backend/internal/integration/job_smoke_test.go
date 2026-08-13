//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

func TestSmoke_Jobs_List(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	client := integrationAuthClient(t, baseURL)

	// List jobs
	resp, err := client.Get(baseURL + "/api/jobs/")
	if err != nil {
		t.Fatalf("List jobs failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("List jobs expected 200, got %d", resp.StatusCode)
	}

	var jobs []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		t.Fatalf("Failed to decode jobs response: %v", err)
	}
	resp.Body.Close()
}
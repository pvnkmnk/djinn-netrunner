//go:build integration

// Integration tests against a real Navidrome server (docker-compose.integration.yml
// service "navidrome-integration", seeded with the fixtures in testdata/navidrome-seed).
// They exercise the same Subsonic-compatible client the acquisition pipeline uses
// in production: authentication, library scan triggering, and Search3 lookup.

package integration

import (
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

func navidromeTestClient(t *testing.T) *services.NavidromeClient {
	t.Helper()

	baseURL := GetEnvOrDefault("INTEGRATION_NAVIDROME_URL", "http://localhost:14533")
	username := GetEnvOrDefault("INTEGRATION_NAVIDROME_USER", "admin")
	password := GetEnvOrDefault("INTEGRATION_NAVIDROME_PASS", "integration-test-pass")

	if os.Getenv("INTEGRATION_NAVIDROME_URL") == "" {
		t.Skip("INTEGRATION_NAVIDROME_URL not set; skipping Navidrome integration test")
	}

	// Plain HTTP client: the compose network is trusted test infrastructure.
	// The production path wires NewProxyAwareHTTPClient(cfg, ...) instead.
	return services.NewNavidromeClient(baseURL, username, password, nil)
}

// TestNavidromeHealthCheck verifies the Subsonic endpoint answers authenticated
// requests — the same request shape stageCheckGonicIndex and the scan trigger use.
func TestNavidromeHealthCheck(t *testing.T) {
	client := navidromeTestClient(t)

	if !client.HealthCheck() {
		t.Fatal("Navidrome health check failed: server unreachable or auth rejected")
	}
}

// TestNavidromeTriggerScan verifies the app's post-download scan trigger works
// against a real Navidrome and that the scan status endpoint reports on it.
func TestNavidromeTriggerScan(t *testing.T) {
	client := navidromeTestClient(t)

	ok, err := client.TriggerScan()
	if err != nil {
		t.Fatalf("TriggerScan failed: %v", err)
	}
	if !ok {
		t.Fatal("TriggerScan returned ok=false")
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		status, err := client.GetScanStatus()
		if err != nil {
			t.Fatalf("GetScanStatus failed: %v", err)
		}
		scanning, _ := status["scanning"].(bool)
		if !scanning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Navidrome scan did not finish within 30s")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestNavidromeSearch3SeededLibrary verifies the dedup path end-to-end: after
// Navidrome scans the seeded fixture folder, Search3 must find the seeded track
// by artist — exactly the check stageCheckGonicIndex performs before downloading.
func TestNavidromeSearch3SeededLibrary(t *testing.T) {
	client := navidromeTestClient(t)

	// Trigger a scan and wait for it to settle so the seed fixtures are indexed.
	if _, err := client.TriggerScan(); err != nil {
		t.Fatalf("TriggerScan failed: %v", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, err := client.GetScanStatus()
		if err != nil {
			t.Fatalf("GetScanStatus failed: %v", err)
		}
		scanning, _ := status["scanning"].(bool)
		count, _ := status["count"].(int)
		if !scanning && count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("seeded library not indexed in time (last status: %v)", status)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Search by the seeded artist, the way the pipeline dedup check does.
	songs, err := client.Search3("Integration Fixture")
	if err != nil {
		t.Fatalf("Search3 failed: %v", err)
	}
	if len(songs) == 0 {
		t.Fatal("Search3 returned no results for seeded artist")
	}
	found := false
	for _, s := range songs {
		if s.Title == "Seed Signal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Search3 results missing seeded track 'Seed Signal': %+v", songs)
	}
}

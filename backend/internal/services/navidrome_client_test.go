package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNavidromeClient_TriggerScan tests the TriggerScan method
func TestNavidromeClient_TriggerScan(t *testing.T) {
	// Create a mock Navidrome server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for required parameters
		if r.URL.Query().Get("u") != "testuser" || r.URL.Query().Get("p") != "testpass" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Return success response
		response := struct {
			SubsonicResponse struct {
				Status string `json:"status"`
			} `json:"subsonic-response"`
		}{
			SubsonicResponse: struct {
				Status string `json:"status"`
			}{
				Status: "ok",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := NewNavidromeClient(server.URL, "testuser", "testpass")

	// Test TriggerScan
	success, err := client.TriggerScan()
	if err != nil {
		t.Fatalf("TriggerScan failed: %v", err)
	}

	if !success {
		t.Error("Expected scan to be triggered successfully")
	}
}

// TestNavidromeClient_HealthCheck tests the HealthCheck method
func TestNavidromeClient_HealthCheck(t *testing.T) {
	// Create a mock server that responds to ping
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := struct {
			SubsonicResponse struct {
				Status string `json:"status"`
			} `json:"subsonic-response"`
		}{
			SubsonicResponse: struct {
				Status string `json:"status"`
			}{
				Status: "ok",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := NewNavidromeClient(server.URL, "testuser", "testpass")

	// Test HealthCheck
	if !client.HealthCheck() {
		t.Error("Expected health check to succeed")
	}
}

// TestPlexClient_TriggerLibraryRefresh tests the TriggerLibraryRefresh method
func TestPlexClient_TriggerLibraryRefresh(t *testing.T) {
	// Create a mock Plex server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for token
		if r.URL.Query().Get("X-Plex-Token") != "test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check for correct path
		if r.URL.Path != "/library/sections/1/refresh" {
			http.NotFound(w, r)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := NewPlexClient(server.URL, "test-token")

	// Test TriggerLibraryRefresh
	err := client.TriggerLibraryRefresh(1)
	if err != nil {
		t.Fatalf("TriggerLibraryRefresh failed: %v", err)
	}
}

// TestJellyfinClient_TriggerLibraryRefresh tests the TriggerLibraryRefresh method
func TestJellyfinClient_TriggerLibraryRefresh(t *testing.T) {
	// Create a mock Jellyfin server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for API key
		if r.Header.Get("X-Emby-Token") != "test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check for correct path and method
		if r.URL.Path != "/Library/Refresh" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := NewJellyfinClient(server.URL, "test-api-key")

	// Test TriggerLibraryRefresh
	err := client.TriggerLibraryRefresh()
	if err != nil {
		t.Fatalf("TriggerLibraryRefresh failed: %v", err)
	}
}

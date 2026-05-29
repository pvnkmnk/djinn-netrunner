package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// TestLidarrProvider_FetchTracks tests the FetchTracks method
func TestLidarrProvider_FetchTracks(t *testing.T) {
	// Create a mock Lidarr server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for API key header
		if r.Header.Get("X-Api-Key") != "test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/api/v1/wanted/missing":
			// Return mock wanted albums
			response := lidarrWantedResponse{
				Records: []lidarrAlbum{
					{ID: 1, Title: "Album 1", ArtistID: 101, ReleaseDate: "2024-01-01"},
					{ID: 2, Title: "Album 2", ArtistID: 102, ReleaseDate: "2024-02-01"},
				},
			}
			json.NewEncoder(w).Encode(response)

		case "/api/v1/artist/101":
			// Return artist 101
			artist := lidarrArtistResponse{ID: 101, Name: "Artist One"}
			json.NewEncoder(w).Encode(artist)

		case "/api/v1/artist/102":
			// Return artist 102
			artist := lidarrArtistResponse{ID: 102, Name: "Artist Two"}
			json.NewEncoder(w).Encode(artist)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create provider with mock server URL
	provider := NewLidarrProvider(server.URL, "test-api-key")

	// Create test watchlist
	watchlist := &database.Watchlist{
		SourceType: "lidarr_wanted",
		SourceURI:  "test",
	}

	// Test FetchTracks
	tracks, snapshotID, err := provider.FetchTracks(context.Background(), watchlist)
	if err != nil {
		t.Fatalf("FetchTracks failed: %v", err)
	}

	// Verify results
	if len(tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(tracks))
	}

	if tracks[0]["artist"] != "Artist One" {
		t.Errorf("Expected artist 'Artist One', got '%s'", tracks[0]["artist"])
	}

	if tracks[0]["title"] != "Album 1" {
		t.Errorf("Expected title 'Album 1', got '%s'", tracks[0]["title"])
	}

	if snapshotID == "" {
		t.Error("Expected non-empty snapshot ID")
	}
}

// TestLidarrProvider_FetchTracks_Unauthorized tests unauthorized access
func TestLidarrProvider_FetchTracks_Unauthorized(t *testing.T) {
	// Create a mock server that returns unauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	// Create provider with wrong API key
	provider := NewLidarrProvider(server.URL, "wrong-key")

	// Create test watchlist
	watchlist := &database.Watchlist{
		SourceType: "lidarr_wanted",
		SourceURI:  "test",
	}

	// Test FetchTracks - should fail
	_, _, err := provider.FetchTracks(context.Background(), watchlist)
	if err == nil {
		t.Error("Expected error for unauthorized access")
	}
}

// TestLidarrProvider_ValidateConfig tests configuration validation
func TestLidarrProvider_ValidateConfig(t *testing.T) {
	// Test with empty base URL
	provider := NewLidarrProvider("", "test-key")
	err := provider.ValidateConfig("")
	if err == nil {
		t.Error("Expected error for empty base URL")
	}

	// Test with valid base URL
	provider = NewLidarrProvider("http://localhost:8686", "test-key")
	err = provider.ValidateConfig("")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

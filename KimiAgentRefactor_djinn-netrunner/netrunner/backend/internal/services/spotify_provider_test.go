package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/interfaces"
	"github.com/zmb3/spotify/v2"
)

// mockSpotifyClientWrapper implements interfaces.SpotifyClientProvider for testing
type mockSpotifyClientWrapper struct {
	client *spotify.Client
}

func (m *mockSpotifyClientWrapper) GetClient(ctx context.Context, userID uint64) (*spotify.Client, error) {
	if m.client == nil {
		return nil, errors.New("client not initialized")
	}
	return m.client, nil
}

// Ensure mockSpotifyClientWrapper implements the required interface
var _ interfaces.SpotifyClientProvider = (*mockSpotifyClientWrapper)(nil)

// createMockSpotifyClient creates a spotify.Client that uses a test server
func createMockSpotifyClient(t *testing.T, handler http.HandlerFunc) *spotify.Client {
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })

	httpClient := server.Client()
	client := spotify.New(httpClient, spotify.WithBaseURL(server.URL+"/"))
	return client
}

func TestSpotifyProvider_FetchTracks(t *testing.T) {
	t.Run("success_playlist", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/playlists/xxx":
				// Playlist metadata request
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"snapshot_id": "snapshot123",
				})
			case "/playlists/xxx/tracks":
				// Playlist tracks request
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]interface{}{
						{
							"type": "track",
							"track": map[string]interface{}{
								"id":   "track1",
								"name": "Karma Police",
								"type": "track",
								"artists": []map[string]interface{}{
									{"name": "Radiohead"},
								},
								"album": map[string]interface{}{
									"name": "OK Computer",
									"images": []map[string]interface{}{
										{"url": "https://example.com/cover.jpg"},
									},
								},
							},
						},
						{
							"type": "track",
							"track": map[string]interface{}{
								"id":   "track2",
								"name": "Creep",
								"type": "track",
								"artists": []map[string]interface{}{
									{"name": "Radiohead"},
								},
								"album": map[string]interface{}{
									"name": "Pablo Honey",
									"images": []map[string]interface{}{
										{"url": "https://example.com/cover2.jpg"},
									},
								},
							},
						},
					},
					"total": 2,
				})
			default:
				http.NotFound(w, r)
			}
		})

		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		results, source, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "spotify_playlist",
			SourceURI:   "spotify:playlist:xxx",
			OwnerUserID: &userID,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if source != "snapshot123" {
			t.Errorf("expected snapshot ID 'snapshot123', got %q", source)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 tracks, got %d", len(results))
		}

		// Verify first track
		if results[0]["artist"] != "Radiohead" {
			t.Errorf("expected artist 'Radiohead', got %q", results[0]["artist"])
		}
		if results[0]["title"] != "Karma Police" {
			t.Errorf("expected title 'Karma Police', got %q", results[0]["title"])
		}
		if results[0]["album"] != "OK Computer" {
			t.Errorf("expected album 'OK Computer', got %q", results[0]["album"])
		}
		if results[0]["cover_art_url"] != "https://example.com/cover.jpg" {
			t.Errorf("expected cover_art_url 'https://example.com/cover.jpg', got %q", results[0]["cover_art_url"])
		}
		if results[0]["id"] != "track1" {
			t.Errorf("expected id 'track1', got %q", results[0]["id"])
		}
	})

	t.Run("success_liked_songs", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/me/tracks":
				// Liked tracks request
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]interface{}{
						{
							"track": map[string]interface{}{
								"id":   "liked1",
								"name": "Yellow",
								"artists": []map[string]interface{}{
									{"name": "Coldplay"},
								},
								"album": map[string]interface{}{
									"name": "Parachutes",
									"images": []map[string]interface{}{
										{"url": "https://example.com/yellow.jpg"},
									},
								},
							},
						},
						{
							"track": map[string]interface{}{
								"id":   "liked2",
								"name": "Clocks",
								"artists": []map[string]interface{}{
									{"name": "Coldplay"},
								},
								"album": map[string]interface{}{
									"name": "A Rush of Blood to the Head",
									"images": []map[string]interface{}{
										{"url": "https://example.com/clocks.jpg"},
									},
								},
							},
						},
					},
					"total": 2,
				})
			default:
				http.NotFound(w, r)
			}
		})

		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		results, source, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "spotify_liked",
			SourceURI:   "spotify:user:xxx:collection",
			OwnerUserID: &userID,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if source == "" {
			t.Error("expected non-empty snapshot ID for liked songs")
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 tracks, got %d", len(results))
		}

		// Verify first track
		if results[0]["artist"] != "Coldplay" {
			t.Errorf("expected artist 'Coldplay', got %q", results[0]["artist"])
		}
		if results[0]["title"] != "Yellow" {
			t.Errorf("expected title 'Yellow', got %q", results[0]["title"])
		}
		if results[0]["album"] != "Parachutes" {
			t.Errorf("expected album 'Parachutes', got %q", results[0]["album"])
		}
		if results[0]["id"] != "liked1" {
			t.Errorf("expected id 'liked1', got %q", results[0]["id"])
		}
	})

	t.Run("error_no_owner", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:         uuid.New(),
			SourceType: "spotify_playlist",
			SourceURI:  "spotify:playlist:xxx",
			// OwnerUserID is nil
		})

		if err == nil {
			t.Fatal("expected error for nil owner, got nil")
		}
		if err.Error() != "watchlist has no owner user" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("error_get_client", func(t *testing.T) {
		mockProvider := &mockSpotifyClientWrapper{
			client: nil, // This will cause GetClient to return error
		}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "spotify_playlist",
			SourceURI:   "spotify:playlist:xxx",
			OwnerUserID: &userID,
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("error_unsupported_source_type", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "unknown_type",
			SourceURI:   "spotify:xxx",
			OwnerUserID: &userID,
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "unsupported spotify source type: unknown_type" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("extract_playlist_id", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {})
		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		tests := []struct {
			uri      string
			expected string
		}{
			{"spotify:playlist:abc123", "abc123"},
			{"https://open.spotify.com/playlist/xyz789?si=123", "xyz789"},
			{"https://open.spotify.com/playlist/xyz789#123", "xyz789"},
			{"plain-id", "plain-id"},
		}

		for _, tc := range tests {
			result := provider.ExtractPlaylistID(tc.uri)
			if result != tc.expected {
				t.Errorf("ExtractPlaylistID(%q) = %q, want %q", tc.uri, result, tc.expected)
			}
		}
	})

	t.Run("empty_playlist", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/playlists/empty":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"snapshot_id": "empty-snapshot",
				})
			case "/playlists/empty/tracks":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]interface{}{},
					"total": 0,
				})
			default:
				http.NotFound(w, r)
			}
		})

		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		results, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "spotify_playlist",
			SourceURI:   "spotify:playlist:empty",
			OwnerUserID: &userID,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 tracks, got %d", len(results))
		}
	})

	t.Run("nil_track_skipped_in_playlist", func(t *testing.T) {
		client := createMockSpotifyClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/playlists/nil":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"snapshot_id": "nil-track-snapshot",
				})
			case "/playlists/nil/tracks":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]interface{}{
						{"type": "track", "track": nil}, // nil track should be skipped
						{
							"type": "track",
							"track": map[string]interface{}{
								"id":   "valid-track",
								"name": "Valid Track",
								"type": "track",
								"artists": []map[string]interface{}{
									{"name": "Artist"},
								},
								"album": map[string]interface{}{
									"name": "Album",
								},
							},
						},
					},
					"total": 2,
				})
			default:
				http.NotFound(w, r)
			}
		})

		mockProvider := &mockSpotifyClientWrapper{client: client}
		provider := NewSpotifyProvider(mockProvider)

		userID := uint64(1)
		results, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
			ID:          uuid.New(),
			SourceType:  "spotify_playlist",
			SourceURI:   "spotify:playlist:nil",
			OwnerUserID: &userID,
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 track (nil skipped), got %d", len(results))
		}
		if results[0]["title"] != "Valid Track" {
			t.Errorf("expected title 'Valid Track', got %q", results[0]["title"])
		}
	})
}

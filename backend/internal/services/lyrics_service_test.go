package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLyricsService_FetchLyrics tests the FetchLyrics method
func TestLyricsService_FetchLyrics(t *testing.T) {
	// Create a mock LRCLIB server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check query parameters
		artist := r.URL.Query().Get("artist_name")
		title := r.URL.Query().Get("track_name")

		if artist == "" || title == "" {
			http.Error(w, "Missing required parameters", http.StatusBadRequest)
			return
		}

		// Return mock lyrics
		results := []Lyrics{
			{
				ID:           1,
				Name:         "Test Song",
				TrackName:    title,
				ArtistName:   artist,
				AlbumName:    "Test Album",
				Duration:     180.0,
				Instrumental: false,
				PlainLyrics:  "Line 1\nLine 2\nLine 3",
				SyncedLyrics: "[00:01.00]Line 1\n[00:02.00]Line 2\n[00:03.00]Line 3",
			},
		}
		json.NewEncoder(w).Encode(results)
	}))
	defer server.Close()

	// Create service with mock server URL
	service := &LyricsService{BaseURL: server.URL + "/api"}

	// Test FetchLyrics
	lyrics, err := service.FetchLyrics(context.Background(), "Test Artist", "Test Song", "Test Album")
	if err != nil {
		t.Fatalf("FetchLyrics failed: %v", err)
	}

	// Verify results
	if lyrics.ArtistName != "Test Artist" {
		t.Errorf("Expected artist 'Test Artist', got '%s'", lyrics.ArtistName)
	}

	if lyrics.TrackName != "Test Song" {
		t.Errorf("Expected track 'Test Song', got '%s'", lyrics.TrackName)
	}

	if lyrics.PlainLyrics == "" {
		t.Error("Expected non-empty plain lyrics")
	}

	if lyrics.SyncedLyrics == "" {
		t.Error("Expected non-empty synced lyrics")
	}
}

// TestLyricsService_FetchLyrics_NotFound tests when no lyrics are found
func TestLyricsService_FetchLyrics_NotFound(t *testing.T) {
	// Create a mock server that returns empty results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty array
		results := []Lyrics{}
		json.NewEncoder(w).Encode(results)
	}))
	defer server.Close()

	// Create service with mock server URL
	service := &LyricsService{BaseURL: server.URL + "/api"}

	// Test FetchLyrics - should fail
	_, err := service.FetchLyrics(context.Background(), "Unknown Artist", "Unknown Song", "")
	if err == nil {
		t.Error("Expected error when no lyrics found")
	}
}

// TestLyricsService_GetSyncedLyrics tests the GetSyncedLyrics method
func TestLyricsService_GetSyncedLyrics(t *testing.T) {
	service := NewLyricsService()

	// Test with synced lyrics
	lyrics := &Lyrics{
		SyncedLyrics: "[00:01.00]Line 1\n[00:02.00]Line 2",
		PlainLyrics:  "Line 1\nLine 2",
	}

	result := service.GetSyncedLyrics(lyrics)
	if result != lyrics.SyncedLyrics {
		t.Error("Expected synced lyrics to be returned")
	}

	// Test with only plain lyrics
	lyrics = &Lyrics{
		PlainLyrics: "Line 1\nLine 2",
	}

	result = service.GetSyncedLyrics(lyrics)
	if result != lyrics.PlainLyrics {
		t.Error("Expected plain lyrics to be returned when synced not available")
	}

	// Test with nil lyrics
	result = service.GetSyncedLyrics(nil)
	if result != "" {
		t.Error("Expected empty string for nil lyrics")
	}
}

// TestLyricsService_IsInstrumental tests the IsInstrumental method
func TestLyricsService_IsInstrumental(t *testing.T) {
	service := NewLyricsService()

	// Test instrumental track
	lyrics := &Lyrics{Instrumental: true}
	if !service.IsInstrumental(lyrics) {
		t.Error("Expected track to be instrumental")
	}

	// Test non-instrumental track
	lyrics = &Lyrics{Instrumental: false}
	if service.IsInstrumental(lyrics) {
		t.Error("Expected track to not be instrumental")
	}

	// Test nil lyrics
	if service.IsInstrumental(nil) {
		t.Error("Expected nil lyrics to not be instrumental")
	}
}

// TestLyricsService_FormatAsLRC tests the FormatAsLRC method
func TestLyricsService_FormatAsLRC(t *testing.T) {
	service := NewLyricsService()

	// Test with synced lyrics
	lyrics := &Lyrics{
		SyncedLyrics: "[00:01.00]Line 1\n[00:02.00]Line 2",
	}

	result := service.FormatAsLRC(lyrics)
	if result != lyrics.SyncedLyrics {
		t.Error("Expected synced lyrics to be returned as LRC")
	}

	// Test with only plain lyrics
	lyrics = &Lyrics{
		PlainLyrics: "Line 1\nLine 2",
	}

	result = service.FormatAsLRC(lyrics)
	if result != "" {
		t.Error("Expected empty string for plain lyrics without timing")
	}

	// Test with nil lyrics
	result = service.FormatAsLRC(nil)
	if result != "" {
		t.Error("Expected empty string for nil lyrics")
	}
}

// TestLyricsService_FormatAsText tests the FormatAsText method
func TestLyricsService_FormatAsText(t *testing.T) {
	service := NewLyricsService()

	// Test with plain lyrics
	lyrics := &Lyrics{
		PlainLyrics: "Line 1\nLine 2",
	}

	result := service.FormatAsText(lyrics)
	if result != lyrics.PlainLyrics {
		t.Error("Expected plain lyrics to be returned")
	}

	// Test with nil lyrics
	result = service.FormatAsText(nil)
	if result != "" {
		t.Error("Expected empty string for nil lyrics")
	}
}

// TestLyricsService_CleanLyrics tests the CleanLyrics method
func TestLyricsService_CleanLyrics(t *testing.T) {
	service := NewLyricsService()

	// Test with messy lyrics
	messy := "Line 1  \r\nLine 2\rLine 3\n"
	expected := "Line 1\nLine 2\nLine 3"

	result := service.CleanLyrics(messy)
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}

	// Test with empty string
	result = service.CleanLyrics("")
	if result != "" {
		t.Error("Expected empty string for empty input")
	}
}

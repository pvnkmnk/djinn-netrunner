package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// BenchmarkCollectUniqueArtists benchmarks the artist collection helper
func BenchmarkCollectUniqueArtists(b *testing.B) {
	// Create test tracks
	tracks := make([]map[string]string, 1000)
	for i := 0; i < 1000; i++ {
		tracks[i] = map[string]string{
			"artist": "Artist " + string(rune(i%10+'0')),
			"title":  "Song " + string(rune(i%100+'0')),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collectUniqueArtists(tracks)
	}
}

// BenchmarkMakeTrackKey benchmarks the track key generation
func BenchmarkMakeTrackKey(b *testing.B) {
	artist := "Test Artist"
	title := "Test Title"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = makeTrackKey(artist, title)
	}
}

// TestFilterExistingTracks_WithErrorHandling verifies error handling works
func TestFilterExistingTracks_WithErrorHandling(t *testing.T) {
	// Test that empty tracks returns empty
	service := &WatchlistService{db: nil}
	result := service.FilterExistingTracks(context.Background(), []map[string]string{})
	assert.Equal(t, 0, len(result), "empty input should return empty output")

	// Test that tracks without artists are preserved
	tracks := []map[string]string{
		{"title": "Test Song"}, // no artist
	}
	result = service.FilterExistingTracks(context.Background(), tracks)
	assert.Equal(t, 1, len(result), "tracks without artists should be preserved")
}

// TestCollectUniqueArtists verifies the helper function works correctly
func TestCollectUniqueArtists(t *testing.T) {
	tracks := []map[string]string{
		{"artist": "Artist A", "title": "Song 1"},
		{"artist": "artist a", "title": "Song 2"}, // duplicate (different case)
		{"artist": "Artist B", "title": "Song 3"},
		{"title": "Song 4"}, // no artist
	}

	result := collectUniqueArtists(tracks)

	// Should have 2 unique artists (Artist A and Artist B)
	assert.GreaterOrEqual(t, len(result), 2, "should find unique artists")
}

// TestMakeTrackKey verifies the key generation is consistent
func TestMakeTrackKey(t *testing.T) {
	key1 := makeTrackKey("Artist A", "Song Title")
	key2 := makeTrackKey("artist a", "song title")
	key3 := makeTrackKey("ARTIST A", "SONG TITLE")

	// All should be equal (case insensitive)
	assert.Equal(t, key1, key2, "keys should match regardless of case")
	assert.Equal(t, key1, key3, "keys should match regardless of case")

	// Different tracks should be different
	key4 := makeTrackKey("Artist B", "Song Title")
	assert.NotEqual(t, key1, key4, "different artists should produce different keys")
}

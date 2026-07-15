package services

import "testing"

func TestIsSpotifySourceType(t *testing.T) {
	tests := []struct {
		name       string
		sourceType string
		want       bool
	}{
		{"spotify_playlist", "spotify_playlist", true},
		{"spotify_liked", "spotify_liked", true},
		{"spotify_discover", "spotify_discover", true},
		{"rss", "rss", false},
		{"lastfm", "lastfm", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSpotifySourceType(tt.sourceType)
			if got != tt.want {
				t.Errorf("isSpotifySourceType(%q) = %v, want %v", tt.sourceType, got, tt.want)
			}
		})
	}
}

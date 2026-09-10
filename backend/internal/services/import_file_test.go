package services

import (
	"path/filepath"
	"strings"
	"testing"
)

// GenerateLibraryPath must prefer the canonical AlbumArtist for the folder so
// multi-credit tracks of one album do not fragment it across artist folders
// (observed live: "Every Time I Die & Daryl Palumbo" splitting an album).
func TestMetadataExtractor_GenerateLibraryPath_AlbumArtistFallback(t *testing.T) {
	e := NewMetadataExtractor()
	libraryRoot := filepath.Join("music", "library")

	tests := []struct {
		name           string
		metadata       *AudioMetadata
		expectedSuffix string
	}{
		{
			name: "album artist wins over per-track credit artist",
			metadata: &AudioMetadata{
				Artist:      "Every Time I Die & Daryl Palumbo",
				AlbumArtist: "Every Time I Die",
				Album:       "Gutter Phenomenon",
				Title:       "Kill The Music",
				Format:      "MP3",
			},
			expectedSuffix: filepath.Join("Every Time I Die", "Gutter Phenomenon", "Kill The Music.mp3"),
		},
		{
			name: "falls back to artist when album artist empty",
			metadata: &AudioMetadata{
				Artist: "Radiohead",
				Album:  "In Rainbows",
				Title:  "Nude",
				Format: "FLAC",
			},
			expectedSuffix: filepath.Join("Radiohead", "In Rainbows", "Nude.flac"),
		},
		{
			name: "album artist is sanitized",
			metadata: &AudioMetadata{
				Artist:      "Guest Feature",
				AlbumArtist: "Artist/With/Slashes",
				Album:       "Album",
				Title:       "Title",
				Format:      "MP3",
			},
			expectedSuffix: filepath.Join("Artist-With-Slashes", "Album", "Title.mp3"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.GenerateLibraryPath(tt.metadata, libraryRoot)
			suffix := strings.TrimPrefix(got, libraryRoot)
			suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
			if suffix != tt.expectedSuffix {
				t.Errorf("GenerateLibraryPath() suffix = %q, want %q", suffix, tt.expectedSuffix)
			}
		})
	}
}

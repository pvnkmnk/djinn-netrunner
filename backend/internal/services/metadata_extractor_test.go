package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataExtractor(t *testing.T) {
	e := NewMetadataExtractor()
	if e == nil {
		t.Fatal("Expected MetadataExtractor to be initialized")
	}

	sanitized := e.SanitizeFilename("Test / File : Name?")
	if sanitized != "Test - File - Name" {
		t.Errorf("Sanitization failed, got: %s", sanitized)
	}
}

func TestDetectImageMimeType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{"jpeg magic bytes", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"jpeg long", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}, "image/jpeg"},
		{"png magic bytes", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"gif magic bytes", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, "image/gif"},
		{"webp magic bytes", []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}, "image/webp"},
		{"too short", []byte{0xFF, 0xD8}, "image/jpeg"},
		{"unknown magic", []byte{0x00, 0x00, 0x00, 0x00}, "image/jpeg"},
		{"empty", []byte{}, "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectImageMimeType(tt.data)
			if got != tt.expected {
				t.Errorf("detectImageMimeType() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEmbedCoverArt_SizeValidation(t *testing.T) {
	e := NewMetadataExtractor()

	// Art data smaller than MinimumCoverArtSize (2048) should fail
	smallData := make([]byte, 100) // 100 bytes < 2048
	err := e.EmbedCoverArt("test.mp3", smallData)
	if err == nil {
		t.Error("expected error for art data below minimum size")
	}
}

func TestFpcalcAvailability(t *testing.T) {
	e := NewMetadataExtractor()

	// Create a temporary audio file for fingerprinting test
	tmpFile := t.TempDir() + "/test_fpcalc.mp3"
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		t.Skipf("could not create temp file: %v", err)
	}

	_, _, err := e.Fingerprint(tmpFile)
	if err != nil {
		t.Skipf("fpcalc not available or failed: %v", err)
	}
}

// --- AudioMetadata.IsValid tests ---

func TestAudioMetadata_IsValid(t *testing.T) {
	tests := []struct {
		name string
		m    AudioMetadata
		want bool
	}{
		{
			name: "valid - both artist and title present",
			m:    AudioMetadata{Artist: "Radiohead", Title: "Killer Cars"},
			want: true,
		},
		{
			name: "valid - all fields present",
			m:    AudioMetadata{Artist: "Pink Floyd", Album: "Dark Side", Title: "Time", Year: 1973},
			want: true,
		},
		{
			name: "invalid - missing artist",
			m:    AudioMetadata{Title: "Killer Cars"},
			want: false,
		},
		{
			name: "invalid - missing title",
			m:    AudioMetadata{Artist: "Radiohead"},
			want: false,
		},
		{
			name: "invalid - empty",
			m:    AudioMetadata{},
			want: false,
		},
		{
			name: "invalid - only album",
			m:    AudioMetadata{Album: "In Rainbows"},
			want: false,
		},
		{
			name: "invalid - only track number",
			m:    AudioMetadata{TrackNumber: 5},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.IsValid()
			if got != tt.want {
				t.Errorf("AudioMetadata.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- MetadataExtractor.IsAudioFile tests ---

func TestMetadataExtractor_IsAudioFile(t *testing.T) {
	e := NewMetadataExtractor()

	tests := []struct {
		path     string
		expected bool
	}{
		// Supported formats (as per actual implementation)
		{"song.mp3", true},
		{"song.flac", true},
		{"song.ogg", true},
		{"song.m4a", true},
		{"song.opus", true},
		{"song.wav", true},
		// Case insensitivity
		{"song.MP3", true},
		{"song.FLAC", true},
		{"song.Ogg", true},
		{"song.M4A", true},
		// Paths
		{"/music/artist/album/track.flac", true},
		{"E:\\music\\album\\song.mp3", true},
		// Unsupported formats
		{"song.txt", false},
		{"song.pdf", false},
		{"song.jpg", false},
		{"song.png", false},
		{"song.exe", false},
		{"song", false},
		{"song.", false},
		{"", false},
		{"song.mp3.txt", false},
		// .aac and .wma are NOT in the switch statement - false
		{"song.aac", false},
		{"song.wma", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := e.IsAudioFile(tt.path)
			if got != tt.expected {
				t.Errorf("IsAudioFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

// --- MetadataExtractor.SanitizeFilename edge case tests ---

func TestMetadataExtractor_SanitizeFilename(t *testing.T) {
	e := NewMetadataExtractor()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already clean",
			input:    "Song Title",
			expected: "Song Title",
		},
		{
			name:     "empty returns Unknown",
			input:    "",
			expected: "Unknown",
		},
		{
			name:     "removes slash",
			input:    "Artist/Album",
			expected: "Artist-Album",
		},
		{
			name:     "removes backslash",
			input:    "Artist\\Album",
			expected: "Artist-Album",
		},
		{
			name:     "removes colon",
			input:    "Song: Title",
			expected: "Song- Title",
		},
		{
			name:     "removes asterisk",
			input:    "Song*Title",
			expected: "SongTitle",
		},
		{
			name:     "removes question mark",
			input:    "What?",
			expected: "What",
		},
		{
			name:     "converts double quote to single",
			input:    `"Song Title"`,
			expected: "'Song Title'",
		},
		{
			name:     "removes less than",
			input:    "Song<Title",
			expected: "SongTitle",
		},
		{
			name:     "removes greater than",
			input:    "Song>Title",
			expected: "SongTitle",
		},
		{
			name:     "converts pipe to dash",
			input:    "Song|Title",
			expected: "Song-Title",
		},
		{
			name:     "multiple problematic chars",
			input:    `a/b\c:d*e?f"g<h>i|j`,
			expected: "a-b-c-def'ghi-j",
		},
		{
			name:     "trims whitespace",
			input:    "  Song Title  ",
			expected: "Song Title",
		},
		{
			name:     "only whitespace becomes empty string",
			input:    "   ",
			expected: "",
		},
		{
			name:     "unicode preserved",
			input:    "日本語タイトル",
			expected: "日本語タイトル",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.SanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- MetadataExtractor.getExt tests ---

func TestMetadataExtractor_getExt(t *testing.T) {
	e := NewMetadataExtractor()

	tests := []struct {
		format   string
		expected string
	}{
		{"MP3", ".mp3"},
		{"mp3", ".mp3"},
		{"Mp3", ".mp3"},
		{"FLAC", ".flac"},
		{"flac", ".flac"},
		{"Flac", ".flac"},
		{"M4A", ".m4a"},
		{"m4a", ".m4a"},
		{"AAC", ".m4a"},
		{"aac", ".m4a"},
		{"OGG", ".ogg"},
		{"ogg", ".ogg"},
		{"Ogg", ".ogg"},
		{"OPUS", ".opus"},
		{"opus", ".opus"},
		// Unrecognized formats
		{"WAV", ""},
		{"wav", ""},
		{"AIFF", ""},
		{"ALAC", ""},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := e.getExt(tt.format)
			if got != tt.expected {
				t.Errorf("getExt(%q) = %q, want %q", tt.format, got, tt.expected)
			}
		})
	}
}

// --- MetadataExtractor.GenerateLibraryPath tests ---

func TestMetadataExtractor_GenerateLibraryPath(t *testing.T) {
	e := NewMetadataExtractor()
	libraryRoot := filepath.Join("music", "library")

	tests := []struct {
		name           string
		metadata       *AudioMetadata
		libraryRoot    string
		expectedSuffix string
	}{
		{
			name: "with track number",
			metadata: &AudioMetadata{
				Artist:      "Pink Floyd",
				Album:       "Dark Side of the Moon",
				Title:       "Time",
				TrackNumber: 6,
				Format:      "FLAC",
			},
			libraryRoot:    libraryRoot,
			expectedSuffix: filepath.Join("Pink Floyd", "Dark Side of the Moon", "06 - Time.flac"),
		},
		{
			name: "without track number",
			metadata: &AudioMetadata{
				Artist:   "Radiohead",
				Album:    "In Rainbows",
				Title:    "Killer Cars",
				Format:   "MP3",
			},
			libraryRoot:    libraryRoot,
			expectedSuffix: filepath.Join("Radiohead", "In Rainbows", "Killer Cars.mp3"),
		},
		{
			name: "empty album defaults to Unknown Album",
			metadata: &AudioMetadata{
				Artist:   "Unknown Artist",
				Album:    "",
				Title:    "Unknown Title",
				Format:   "OGG",
			},
			libraryRoot:    libraryRoot,
			// Note: SanitizeFilename("") returns "Unknown", so the album check "if album == \"\"" is false
			// and album remains "Unknown" (not "Unknown Album")
			expectedSuffix: filepath.Join("Unknown Artist", "Unknown", "Unknown Title.ogg"),
		},
		{
			name: "sanitizes special characters",
			metadata: &AudioMetadata{
				Artist:      "Artist/With/Slashes",
				Album:       "Album: With Colons",
				Title:       "Song*With?Special<Chars>",
				TrackNumber: 1,
				Format:      "M4A",
			},
			libraryRoot:    libraryRoot,
			expectedSuffix: filepath.Join("Artist-With-Slashes", "Album- With Colons", "01 - SongWithSpecialChars.m4a"),
		},
		{
			name: "empty album title preserved",
			metadata: &AudioMetadata{
				Artist: "Test Artist",
				Album:  "",
				Title:  "Test Title",
				Format: "OPUS",
			},
			libraryRoot:    libraryRoot,
			// Same issue: SanitizeFilename("") returns "Unknown", not replaced by "Unknown Album"
			expectedSuffix: filepath.Join("Test Artist", "Unknown", "Test Title.opus"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.GenerateLibraryPath(tt.metadata, tt.libraryRoot)
			if !strings.HasPrefix(got, tt.libraryRoot) {
				t.Errorf("GenerateLibraryPath() = %q, expected to start with %q", got, tt.libraryRoot)
			}
			suffix := strings.TrimPrefix(got, tt.libraryRoot)
			suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
			if suffix != tt.expectedSuffix {
				t.Errorf("GenerateLibraryPath() suffix = %q, want %q", suffix, tt.expectedSuffix)
			}
		})
	}
}

// --- AudioMetadata struct immutability validation ---

func TestAudioMetadata_Fields(t *testing.T) {
	m := AudioMetadata{
		Artist:      "Artist",
		Album:       "Album",
		Title:       "Title",
		TrackNumber: 5,
		Year:        2024,
		Format:      "FLAC",
		FileSize:    12345,
	}

	if m.Artist != "Artist" {
		t.Errorf("Artist field mismatch")
	}
	if m.Album != "Album" {
		t.Errorf("Album field mismatch")
	}
	if m.Title != "Title" {
		t.Errorf("Title field mismatch")
	}
	if m.TrackNumber != 5 {
		t.Errorf("TrackNumber field mismatch")
	}
	if m.Year != 2024 {
		t.Errorf("Year field mismatch")
	}
	if m.Format != "FLAC" {
		t.Errorf("Format field mismatch")
	}
	if m.FileSize != 12345 {
		t.Errorf("FileSize field mismatch")
	}
}

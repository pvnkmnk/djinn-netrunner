package services

import (
	"os"
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

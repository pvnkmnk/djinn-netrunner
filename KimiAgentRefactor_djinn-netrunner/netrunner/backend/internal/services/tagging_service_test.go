package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTaggingService_AcoustIDConfidence tests the confidence threshold validation
func TestTaggingService_AcoustIDConfidence(t *testing.T) {
	service := NewTaggingService()

	// Test default threshold
	if service.GetAcoustIDMinConfidence() != 0.7 {
		t.Errorf("Expected default threshold 0.7, got %f", service.GetAcoustIDMinConfidence())
	}

	// Test validation with different confidence levels
	tests := []struct {
		confidence float64
		expected   bool
	}{
		{0.8, true},  // Above threshold
		{0.7, true},  // At threshold
		{0.6, false}, // Below threshold
		{0.0, false}, // Zero
		{1.0, true},  // Perfect score
	}

	for _, test := range tests {
		result := service.ValidateAcoustIDConfidence(test.confidence)
		if result != test.expected {
			t.Errorf("Confidence %f: expected %v, got %v", test.confidence, test.expected, result)
		}
	}

	// Test setting custom threshold
	service.SetAcoustIDMinConfidence(0.8)
	if service.GetAcoustIDMinConfidence() != 0.8 {
		t.Errorf("Expected threshold 0.8, got %f", service.GetAcoustIDMinConfidence())
	}

	// Test validation with new threshold
	if !service.ValidateAcoustIDConfidence(0.9) {
		t.Error("Expected 0.9 to pass with threshold 0.8")
	}
	if service.ValidateAcoustIDConfidence(0.7) {
		t.Error("Expected 0.7 to fail with threshold 0.8")
	}
}

// TestTaggingService_CalculateFileHash tests the file hash calculation
func TestTaggingService_CalculateFileHash(t *testing.T) {
	service := NewTaggingService()

	// Create a temporary file for testing
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Calculate hash
	hash, err := service.CalculateFileHash(testFile)
	if err != nil {
		t.Fatalf("CalculateFileHash failed: %v", err)
	}

	// Verify hash is not empty
	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Verify hash is consistent
	hash2, err := service.CalculateFileHash(testFile)
	if err != nil {
		t.Fatalf("CalculateFileHash failed on second call: %v", err)
	}

	if hash != hash2 {
		t.Errorf("Hashes don't match: %s != %s", hash, hash2)
	}

	// Test with non-existent file
	_, err = service.CalculateFileHash("/non/existent/file.txt")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

// TestTaggingService_Provenance tests the provenance marshaling/unmarshaling
func TestTaggingService_Provenance(t *testing.T) {
	service := NewTaggingService()

	// Test marshaling
	provenance := &EnrichmentProvenance{
		Title:    "musicbrainz",
		Artist:   "musicbrainz",
		Album:    "musicbrainz",
		Year:     "discogs",
		Genre:    "discogs",
		Composer: "acoustid",
		CoverArt: "coverartarchive",
	}

	jsonStr, err := service.MarshalProvenance(provenance)
	if err != nil {
		t.Fatalf("MarshalProvenance failed: %v", err)
	}

	if jsonStr == "" {
		t.Error("Expected non-empty JSON string")
	}

	// Test unmarshaling
	parsed, err := service.UnmarshalProvenance(jsonStr)
	if err != nil {
		t.Fatalf("UnmarshalProvenance failed: %v", err)
	}

	if parsed.Title != provenance.Title {
		t.Errorf("Expected title source '%s', got '%s'", provenance.Title, parsed.Title)
	}

	if parsed.Artist != provenance.Artist {
		t.Errorf("Expected artist source '%s', got '%s'", provenance.Artist, parsed.Artist)
	}

	// Test with empty string
	parsed, err = service.UnmarshalProvenance("")
	if err != nil {
		t.Fatalf("UnmarshalProvenance failed with empty string: %v", err)
	}
	if parsed != nil {
		t.Error("Expected nil for empty string")
	}

	// Test marshaling nil
	jsonStr, err = service.MarshalProvenance(nil)
	if err != nil {
		t.Fatalf("MarshalProvenance failed with nil: %v", err)
	}
	if jsonStr != "" {
		t.Error("Expected empty string for nil provenance")
	}
}

// TestTaggingService_MergeProvenance tests the provenance merging
func TestTaggingService_MergeProvenance(t *testing.T) {
	service := NewTaggingService()

	base := &EnrichmentProvenance{
		Title:  "musicbrainz",
		Artist: "musicbrainz",
		Album:  "musicbrainz",
	}

	override := &EnrichmentProvenance{
		Title: "discogs",
		Genre: "discogs",
	}

	merged := service.MergeProvenance(base, override)

	// Title should be overridden
	if merged.Title != "discogs" {
		t.Errorf("Expected title 'discogs', got '%s'", merged.Title)
	}

	// Artist should remain from base
	if merged.Artist != "musicbrainz" {
		t.Errorf("Expected artist 'musicbrainz', got '%s'", merged.Artist)
	}

	// Album should remain from base
	if merged.Album != "musicbrainz" {
		t.Errorf("Expected album 'musicbrainz', got '%s'", merged.Album)
	}

	// Genre should be added from override
	if merged.Genre != "discogs" {
		t.Errorf("Expected genre 'discogs', got '%s'", merged.Genre)
	}

	// Test with nil base
	merged = service.MergeProvenance(nil, override)
	if merged != override {
		t.Error("Expected override to be returned when base is nil")
	}

	// Test with nil override
	merged = service.MergeProvenance(base, nil)
	if merged != base {
		t.Error("Expected base to be returned when override is nil")
	}
}

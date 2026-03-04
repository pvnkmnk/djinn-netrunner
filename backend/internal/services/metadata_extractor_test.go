package services

import (
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

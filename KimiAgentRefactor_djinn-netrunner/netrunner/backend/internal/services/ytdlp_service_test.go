package services

import (
	"os"
	"testing"
)

// TestYtdlpService_DownloadAudio tests the DownloadAudio method
func TestYtdlpService_DownloadAudio(t *testing.T) {
	s := NewYtdlpService()

	// Test yt-dlp availability - skip if not available
	if !s.IsYtdlpAvailable() {
		t.Skip("yt-dlp not available, skipping yt-dlp tests")
	}

	// Skip this test in CI or when network access is not available
	// This test requires network access and may be slow
	if os.Getenv("CI") != "" || os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network test in CI environment")
	}

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Test with a valid YouTube URL (using a short test video)
	// Note: This test requires network access and yt-dlp to be installed
	// In CI, this test should be skipped or mocked
	testURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ" // Rick Astley - Never Gonna Give You Up

	// Test downloading audio
	outputPath, err := s.DownloadAudio(testURL, tmpDir, "mp3")
	if err != nil {
		t.Fatalf("DownloadAudio failed: %v", err)
	}
	defer os.Remove(outputPath) // Clean up

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("Output file was not created: %v", err)
	}

	// Verify file has content
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Failed to stat output file: %v", err)
	}
	if fileInfo.Size() == 0 {
		t.Error("Output file is empty")
	}
}

// TestYtdlpService_DownloadAudio_InvalidInput tests error handling
func TestYtdlpService_DownloadAudio_InvalidInput(t *testing.T) {
	s := NewYtdlpService()

	// Test empty URL
	_, err := s.DownloadAudio("", "/tmp", "mp3")
	if err == nil {
		t.Error("Expected error for empty URL")
	}

	// Test empty output directory
	_, err = s.DownloadAudio("https://example.com", "", "mp3")
	if err == nil {
		t.Error("Expected error for empty output directory")
	}

	// Test non-existent output directory
	_, err = s.DownloadAudio("https://example.com", "/non/existent/dir", "mp3")
	if err == nil {
		t.Error("Expected error for non-existent output directory")
	}
}

// TestYtdlpService_IsYtdlpAvailable tests the availability check
func TestYtdlpService_IsYtdlpAvailable(t *testing.T) {
	s := NewYtdlpService()
	// This test just ensures the method doesn't panic
	// The actual availability depends on the system
	_ = s.IsYtdlpAvailable()
}

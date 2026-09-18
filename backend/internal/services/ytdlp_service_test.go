package services

import (
	"os"
	"strings"
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

// TestCheckPublicHost pins the judgement the fallback shares with the request-path
// guard. IP literals keep it deterministic: no DNS, no network.
func TestCheckPublicHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		refused bool
	}{
		{"loopback", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"metadata service", "169.254.169.254", true},
		{"rfc1918", "10.0.0.5", true},
		{"docker network peer", "172.17.0.2", true},
		{"public", "93.184.216.34", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPublicHost(tt.host)
			if tt.refused && err == nil {
				t.Fatalf("checkPublicHost(%q) allowed a private destination", tt.host)
			}
			if !tt.refused && err != nil {
				t.Fatalf("checkPublicHost(%q) refused a public destination: %v", tt.host, err)
			}
		})
	}
}

// A feed can put anything in source_url, and the fallback now reaches yt-dlp for
// every item that carries one. Without the destination check this test fails on
// the error message (the call proceeds to exec instead of refusing), which is the
// mutation this guards.
func TestYtdlpService_DownloadAudio_RefusesPrivateTargets(t *testing.T) {
	s := NewYtdlpService()
	dir := t.TempDir()

	targets := []string{
		"http://127.0.0.1:8080/admin",
		"http://localhost:5030/api/v0/transfers",
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"http://10.0.0.5/track.mp3",
		"http://[::1]/track.mp3",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			_, err := s.DownloadAudio(target, dir, "flac")
			if err == nil {
				t.Fatalf("DownloadAudio(%q) was handed to yt-dlp instead of refused", target)
			}
			if !strings.Contains(err.Error(), "refusing source URL") || !strings.Contains(err.Error(), "private IP") {
				t.Fatalf("DownloadAudio(%q) refused for the wrong reason: %v", target, err)
			}
		})
	}
}

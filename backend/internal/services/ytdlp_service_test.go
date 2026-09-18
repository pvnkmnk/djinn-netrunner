package services

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	outputPath, err := s.DownloadAudio(context.Background(), testURL, tmpDir, "mp3")
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
	_, err := s.DownloadAudio(context.Background(), "", "/tmp", "mp3")
	if err == nil {
		t.Error("Expected error for empty URL")
	}

	// Test empty output directory
	_, err = s.DownloadAudio(context.Background(), "https://example.com", "", "mp3")
	if err == nil {
		t.Error("Expected error for empty output directory")
	}

	// Test non-existent output directory
	_, err = s.DownloadAudio(context.Background(), "https://example.com", "/non/existent/dir", "mp3")
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
			_, err := s.DownloadAudio(context.Background(), target, dir, "flac")
			if err == nil {
				t.Fatalf("DownloadAudio(%q) was handed to yt-dlp instead of refused", target)
			}
			if !strings.Contains(err.Error(), "refusing source URL") || !strings.Contains(err.Error(), "private IP") {
				t.Fatalf("DownloadAudio(%q) refused for the wrong reason: %v", target, err)
			}
			// The pipeline treats a guard refusal as terminal and an ordinary
			// download failure as retryable, so the type matters, not just the text.
			if !errors.Is(err, ErrDisallowedDestination) {
				t.Fatalf("DownloadAudio(%q) was not refused as a disallowed destination: %v", target, err)
			}
		})
	}
}

// A name that will not resolve is not a refusal. It has to stay an ordinary
// failure so a transient DNS outage retries — but the URL is still never handed
// to the downloader unchecked.
func TestYtdlpService_DownloadAudio_UnresolvableHostIsNotARefusal(t *testing.T) {
	s := NewYtdlpService()

	_, err := s.DownloadAudio(context.Background(), "http://netrunner-nonexistent-host.invalid/track.mp3", t.TempDir(), "flac")

	require.Error(t, err)
	if errors.Is(err, ErrDisallowedDestination) {
		t.Fatalf("an unresolvable host must not be reported as a guard refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "could not resolve") {
		t.Fatalf("the failure must say the chain could not be resolved: %v", err)
	}
}

// The walk has to happen on the real path, not only in the walker's own tests:
// a refusal on a *redirect target* must reach the caller as a guard refusal.
// Deleting the walk from DownloadAudio turns this red, because the private hop
// stops being seen at all.
func TestYtdlpService_DownloadAudio_RefusesAPrivateRedirectTarget(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/start": {status: http.StatusFound, location: "http://10.0.0.5/secret"},
	}}
	s := NewYtdlpService()
	s.resolveClient = guardedClient(canned)

	_, err := s.DownloadAudio(context.Background(), "http://93.184.216.34/start", t.TempDir(), "flac")

	require.Error(t, err)
	if !errors.Is(err, ErrDisallowedDestination) {
		t.Fatalf("a private redirect target must be refused as a disallowed destination: %v", err)
	}
	if !strings.Contains(err.Error(), "refusing source URL") {
		t.Fatalf("the refusal must name what it refused: %v", err)
	}
	// The guard refuses the hop before it is even attempted, so the private URL
	// never reaches the transport — the point is that it is not followed.
	if strings.Contains(strings.Join(canned.seen, " "), "10.0.0.5") {
		t.Fatalf("the refused hop must not be followed, got %v", canned.seen)
	}
}

// And the chain is walked before the downloader runs: with a legitimate chain,
// the guard requests the redirect target itself. Deleting the walk leaves this
// list empty (the private-target case above would also go quiet).
func TestYtdlpService_DownloadAudio_WalksTheChainBeforeHandover(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/start": {status: http.StatusMovedPermanently, location: "http://93.184.216.35/real.mp3"},
	}}
	s := NewYtdlpService()
	s.resolveClient = guardedClient(canned)

	// Ignored: yt-dlp is not installed on the test host, so the download itself
	// fails after the walk. The walk is what this asserts.
	_, _ = s.DownloadAudio(context.Background(), "http://93.184.216.34/start", t.TempDir(), "flac")

	require.Contains(t, canned.seen, "http://93.184.216.35/real.mp3",
		"the redirect target must be requested by the guard before the downloader runs")
}

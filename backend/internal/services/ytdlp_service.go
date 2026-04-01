package services

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// YtdlpService handles yt-dlp based audio extraction
type YtdlpService struct{}

// NewYtdlpService creates a new yt-dlp service
func NewYtdlpService() *YtdlpService {
	return &YtdlpService{}
}

// DownloadAudio extracts audio from a URL using yt-dlp
func (s *YtdlpService) DownloadAudio(rawURL, outputDir, audioFormat string) (string, error) {
	// Validate input
	if rawURL == "" {
		return "", errors.New("URL is required")
	}
	if outputDir == "" {
		return "", errors.New("output directory is required")
	}

	// SECURITY: Validate URL format to prevent command injection
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("URL must use http or https scheme")
	}
	// Reconstruct URL from parsed components to ensure it's clean
	url := parsed.String()

	// Check if output directory exists
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		return "", errors.New("output directory does not exist")
	}

	// Default to FLAC if no format specified
	if audioFormat == "" {
		audioFormat = "flac"
	}

	// Validate audio format against whitelist
	validFormats := map[string]bool{"flac": true, "mp3": true, "wav": true, "aac": true, "ogg": true, "m4a": true, "opus": true}
	if !validFormats[audioFormat] {
		return "", fmt.Errorf("unsupported audio format: %s (supported: flac, mp3, wav, aac, ogg, m4a, opus)", audioFormat)
	}

	// Generate output template
	outputTemplate := filepath.Join(outputDir, "%(title)s.%(ext)s")

	// Build yt-dlp command with audio extraction flags
	// SECURITY: All arguments are passed as separate slice elements, never concatenated into a shell string
	cmd := exec.Command("yt-dlp",
		"--extract-audio",
		"--audio-format", audioFormat,
		"--output", outputTemplate,
		"--no-playlist",
		"--print", "after_move:filepath",
		url,
	)

	// Run command and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp failed: %w: %s", err, string(output))
	}

	// Parse output to find downloaded file
	// yt-dlp with --print after_move:filepath outputs the final file path
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return "", errors.New("yt-dlp completed but no output file detected")
	}

	// The output should be the downloaded file path
	downloadedFile := strings.TrimSpace(outputStr)

	// Verify the file exists
	if _, err := os.Stat(downloadedFile); os.IsNotExist(err) {
		return "", fmt.Errorf("downloaded file not found: %s", downloadedFile)
	}

	return downloadedFile, nil
}

// IsYtdlpAvailable checks if yt-dlp is installed and accessible
func (s *YtdlpService) IsYtdlpAvailable() bool {
	_, err := exec.LookPath("yt-dlp")
	return err == nil
}

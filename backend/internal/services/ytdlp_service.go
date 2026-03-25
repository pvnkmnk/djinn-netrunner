package services

import (
	"errors"
	"fmt"
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
func (s *YtdlpService) DownloadAudio(url, outputDir, audioFormat string) (string, error) {
	// Validate input
	if url == "" {
		return "", errors.New("URL is required")
	}
	if outputDir == "" {
		return "", errors.New("output directory is required")
	}

	// Check if output directory exists
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		return "", errors.New("output directory does not exist")
	}

	// Default to FLAC if no format specified
	if audioFormat == "" {
		audioFormat = "flac"
	}

	// Generate output template
	outputTemplate := filepath.Join(outputDir, "%(title)s.%(ext)s")

	// Build yt-dlp command with audio extraction flags
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

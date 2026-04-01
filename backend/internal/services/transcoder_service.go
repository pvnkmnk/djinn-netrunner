package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TranscoderService handles FFmpeg-based audio transcoding
type TranscoderService struct{}

// NewTranscoderService creates a new transcoder service
func NewTranscoderService() *TranscoderService {
	return &TranscoderService{}
}

// Transcode converts an audio file to the specified format using FFmpeg
func (s *TranscoderService) Transcode(inputPath, outputFormat string) (string, error) {
	// Validate input
	if inputPath == "" {
		return "", errors.New("input path is required")
	}
	if outputFormat == "" {
		return "", errors.New("output format is required")
	}

	// Check if input file exists
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return "", errors.New("input file does not exist")
	}

	// SECURITY: Validate output format against whitelist to prevent command injection
	validFormats := map[string]bool{
		"mp3": true, "flac": true, "wav": true, "aac": true,
		"ogg": true, "m4a": true, "opus": true, "wma": true,
	}
	if !validFormats[outputFormat] {
		return "", fmt.Errorf("unsupported output format: %s", outputFormat)
	}

	// Generate output path by replacing extension
	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "." + outputFormat

	// SECURITY: Validate output path stays within expected directory
	cleanOutput := filepath.Clean(outputPath)
	if !strings.HasPrefix(cleanOutput, filepath.Clean(filepath.Dir(inputPath))) {
		return "", errors.New("output path would escape input directory")
	}

	// Build FFmpeg command
	// SECURITY: All arguments passed as separate slice elements, never concatenated
	cmd := exec.Command("ffmpeg", "-i", inputPath, "-y", outputPath)

	// Run command and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("transcode failed: %w: %s", err, string(output))
	}

	return outputPath, nil
}

// IsFFmpegAvailable checks if FFmpeg is installed and accessible
func (s *TranscoderService) IsFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

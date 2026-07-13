package services

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TranscoderService handles FFmpeg-based audio transcoding
type TranscoderService struct {
	FFmpegPath string
}

// NewTranscoderService creates a new transcoder service
func NewTranscoderService() *TranscoderService {
	return &TranscoderService{FFmpegPath: "ffmpeg"}
}

// NewTranscoderServiceWithFFmpeg creates a new transcoder service with a custom FFmpeg path
func NewTranscoderServiceWithFFmpeg(ffmpegPath string) *TranscoderService {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &TranscoderService{FFmpegPath: ffmpegPath}
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

	// SECURITY: Validate output path stays within expected directory using filepath.Rel
	// This properly handles edge cases like "../" escapes and symlinks
	inputDir := filepath.Dir(inputPath)
	relPath, err := filepath.Rel(inputDir, outputPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", errors.New("output path would escape input directory")
	}
	// Additional check: ensure the resolved path is still within input directory
	resolvedOutput := filepath.Join(inputDir, relPath)
	if filepath.Clean(resolvedOutput) != filepath.Clean(outputPath) {
		return "", errors.New("output path would escape input directory")
	}

	// Build FFmpeg command
	// SECURITY: All arguments passed as separate slice elements, never concatenated
	cmd := exec.Command(s.FFmpegPath, "-i", inputPath, "-y", outputPath)

	// Run command and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("transcode failed: %w: %s", err, string(output))
	}

	return outputPath, nil
}

// IsFFmpegAvailable checks if FFmpeg is installed and accessible
func (s *TranscoderService) IsFFmpegAvailable() bool {
	_, err := exec.LookPath(s.FFmpegPath)
	return err == nil
}

// StreamTranscode transcodes an audio file and streams the output to the provided writer.
// It uses FFmpeg to pipe the transcoded audio directly without writing to disk.
func (s *TranscoderService) StreamTranscode(inputPath, outputFormat string, bitrate int, w io.Writer) error {
	if inputPath == "" {
		return errors.New("input path is required")
	}
	if outputFormat == "" {
		return errors.New("output format is required")
	}

	// Validate input file exists
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return errors.New("input file does not exist")
	}

	// Validate output format
	validFormats := map[string]bool{
		"mp3": true, "flac": true, "wav": true, "aac": true,
		"ogg": true, "m4a": true, "opus": true,
	}
	if !validFormats[outputFormat] {
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}

	// Build FFmpeg command for streaming output
	args := []string{
		"-i", inputPath,
		"-f", outputFormat,
	}

	// Add bitrate for lossy formats
	if bitrate > 0 && isLossyFormat(outputFormat) {
		args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate))
	}

	args = append(args, "-v", "quiet", "pipe:1")

	cmd := exec.Command(s.FFmpegPath, args...)
	cmd.Stdout = w

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("transcode failed: %w: %s", err, stderr.String())
	}

	return nil
}

// isLossyFormat returns true if the format is lossy (supports bitrate control)
func isLossyFormat(format string) bool {
	switch format {
	case "mp3", "aac", "ogg", "opus", "m4a":
		return true
	}
	return false
}

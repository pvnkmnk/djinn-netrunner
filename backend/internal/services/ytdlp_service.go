package services

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// YtdlpService handles yt-dlp based audio extraction
type YtdlpService struct {
	ytdlpPath string // path to yt-dlp binary
	jsRuntime string // JS runtime for yt-dlp extraction (e.g., "node")
}

// NewYtdlpService creates a new yt-dlp service with auto-detected binary path
func NewYtdlpService() *YtdlpService {
	path := os.Getenv("YTDLP_PATH")
	if path == "" {
		path = "yt-dlp"
	}
	// Detect available JS runtime for yt-dlp's JavaScript extraction.
	// yt-dlp 2026+ requires an explicit --js-runtimes flag.
	jsRuntime := os.Getenv("YTDLP_JS_RUNTIME")
	if jsRuntime == "" {
		if _, err := exec.LookPath("node"); err == nil {
			jsRuntime = "node"
		}
	}
	return &YtdlpService{ytdlpPath: path, jsRuntime: jsRuntime}
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

	// Generate output template — use video ID as base filename to avoid
	// filesystem issues with very long YouTube titles
	outputTemplate := filepath.Join(outputDir, "%(id)s.%(ext)s")

	// Build yt-dlp command with audio extraction flags
	// SECURITY: All arguments are passed as separate slice elements, never concatenated into a shell string
	args := []string{
		"--extract-audio",
		"--audio-format", audioFormat,
		"--output", outputTemplate,
		"--no-playlist",
		"--no-split-chapters",
		"--print", "after_move:filepath",
	}
	if s.jsRuntime != "" {
		args = append(args, "--js-runtimes", s.jsRuntime)
		// yt-dlp 2026+ uses remote component solvers for JS challenges
		args = append(args, "--remote-components", "ejs:github")
	}
	args = append(args, "--", url)

	// SECURITY: s.ytdlpPath is set from YTDLP_PATH env var at startup (not user input).
	// All user-supplied values (URL, format) are validated/whitelisted above.
	// The "--" separator before the URL prevents argument injection.
	cmd := exec.Command(s.ytdlpPath, args...)

	// Capture stdout and stderr separately -- yt-dlp may emit warnings on stderr
	// (e.g. "your version is old") that would pollute the --print filepath on stdout.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if outMsg := strings.TrimSpace(stdout.String()); outMsg != "" {
			errMsg = outMsg + ": " + errMsg
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("yt-dlp failed: %s", errMsg)
	}

	// Parse output to find downloaded file
	// yt-dlp with --print after_move:filepath outputs the final file path on stdout
	outputStr := strings.TrimSpace(stdout.String())
	if outputStr == "" {
		return "", errors.New("yt-dlp completed but no output file detected")
	}

	// The output should be the downloaded file path
	downloadedFile := outputStr

	// Verify the file exists
	if _, err := os.Stat(downloadedFile); os.IsNotExist(err) {
		return "", fmt.Errorf("downloaded file not found: %s", downloadedFile)
	}

	return downloadedFile, nil
}

// IsYtdlpAvailable checks if yt-dlp is installed and accessible
func (s *YtdlpService) IsYtdlpAvailable() bool {
	_, err := exec.LookPath(s.ytdlpPath)
	return err == nil
}

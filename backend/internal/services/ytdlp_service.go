package services

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// targetResolveTimeout bounds the pre-flight walk of a source URL's redirect
// chain. It is deliberately short: it resolves a destination, it does not download.
const targetResolveTimeout = 20 * time.Second

// YtdlpService handles yt-dlp based audio extraction
type YtdlpService struct {
	ytdlpPath string // path to yt-dlp binary
	jsRuntime string // JS runtime for yt-dlp extraction (e.g., "node")

	// resolveClient walks a source URL's redirect chain before handover. Nil
	// means the production client (safe transport plus hop bound); tests set it
	// to exercise the walk without a network, so removing the walk from
	// DownloadAudio is visible to the suite rather than silently safe-looking.
	resolveClient *http.Client
}

// targetClient is the client the pre-flight walk uses.
func (s *YtdlpService) targetClient() *http.Client {
	if s.resolveClient != nil {
		return s.resolveClient
	}
	return newRedirectResolvingClient(targetResolveTimeout)
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
	// SECURITY: yt-dlp makes its own connections, so the repository's safe
	// transports never see them, and it follows redirects on its own. A source
	// URL here comes from a watchlist feed (jobitems.source_url), so it is
	// attacker-influenced, which is why the destination is checked twice: the
	// host the URL names, and then the chain that host answers with.
	if err := checkPublicHost(parsed.Hostname()); err != nil {
		if errors.Is(err, ErrDisallowedDestination) {
			return "", fmt.Errorf("refusing source URL: %w", err)
		}
		// Not a refusal, just a name that would not resolve. Still not a URL to
		// hand over unchecked.
		return "", fmt.Errorf("could not resolve source URL host %q: %w", parsed.Hostname(), err)
	}

	// SECURITY: the first hop being public says nothing about where the download
	// ends up, because the extractor follows redirects itself. Walk the chain
	// here first — every hop dialed through safeDialContext — and hand over the
	// URL that was actually checked. A hop that resolves privately refuses the
	// download; a chain that cannot be walked at all is also not handed over,
	// since the guard cannot vouch for a hop it never saw.
	//
	// What this cannot see: redirects the downloader encounters on its own after
	// handover. yt-dlp exposes no hop bound (`--max-redirects` does not exist) and
	// a `--proxy` would mean running a validating proxy, which is an egress
	// boundary rather than a check at this seam. It is recorded as a decision for
	// the single-operator beta — the feed URLs are the operator's own — and it is
	// the point to revisit before this entrance serves untrusted feeds.
	resolved, err := resolveRedirectTarget(s.targetClient(), parsed.String())
	if err != nil {
		if errors.Is(err, ErrDisallowedDestination) {
			return "", fmt.Errorf("refusing source URL: %w", err)
		}
		return "", fmt.Errorf("could not resolve %q before downloading it: %w", parsed.Host, err)
	}

	// The resolved URL is what the downloader is given: it is the chain member
	// that was checked, and asking the extractor to follow the chain again would
	// be asking it to walk hops this guard never saw.
	url := resolved

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

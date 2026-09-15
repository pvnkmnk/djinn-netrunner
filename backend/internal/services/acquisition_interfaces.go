package services

import (
	"context"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// SlskdClient defines the interface for Soulseek daemon operations used by the acquisition pipeline.
type SlskdClient interface {
	Search(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error)
	Browse(username string) ([]PeerFile, error)
	EnqueueDownload(username, filename string, size int64) (string, error)
	WaitForDownload(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error)
}

// DownloadWaitOptions configures a single WaitForDownload call.
type DownloadWaitOptions struct {
	// Timeout bounds the whole transfer.
	Timeout time.Duration
	// HasAlternatives reports whether another candidate can be tried if this
	// peer fails. When true, a transfer that sits queued without ever starting
	// is abandoned after remoteQueueGrace rather than consuming the full
	// timeout. When false — the last candidate — the wait is allowed to run its
	// course, because abandoning it saves nothing and would fail an item that
	// waiting might still complete.
	HasAlternatives bool
}

// SubsonicClientInterface defines the interface for Subsonic-compatible library
// index operations (Navidrome, Gonic, Airsonic, …).
type SubsonicClientInterface interface {
	Search3(query string) ([]SubsonicSong, error)
	TriggerScan() (bool, error)
}


// YtdlpClientInterface defines the interface for yt-dlp audio extraction operations.
type YtdlpClientInterface interface {
	DownloadAudio(rawURL, outputDir, audioFormat string) (string, error)
	IsYtdlpAvailable() bool
}

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
	WaitForDownload(ctx context.Context, username, downloadID string, timeout time.Duration) (*Download, error)
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

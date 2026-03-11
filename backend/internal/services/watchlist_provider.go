package services

import (
	"context"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// WatchlistProvider defines the interface for different music discovery sources
type WatchlistProvider interface {
	// FetchTracks retrieves the current tracks from the source
	FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error)

	// ValidateConfig checks if the provided configuration is valid for this provider
	ValidateConfig(config string) error
}

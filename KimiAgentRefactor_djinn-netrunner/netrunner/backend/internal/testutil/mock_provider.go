package testutil

import (
	"context"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/interfaces"
)

// Compile-time interface check
var _ interfaces.WatchlistProvider = (*MockProvider)(nil)

// MockProvider is a test double for interfaces.WatchlistProvider.
// It returns pre-configured tracks, snapshot ID, and error.
type MockProvider struct {
	Tracks []map[string]string
	SnapID string
	Err    error
}

func (m *MockProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	return m.Tracks, m.SnapID, m.Err
}

func (m *MockProvider) ValidateConfig(config string) error {
	return nil
}

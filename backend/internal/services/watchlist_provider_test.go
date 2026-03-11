package services

import (
	"context"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

type MockProvider struct {
	tracks []map[string]string
	snapID string
	err    error
}

func (m *MockProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	return m.tracks, m.snapID, m.err
}

func (m *MockProvider) ValidateConfig(config string) error {
	return nil
}

func TestProviderInterface(t *testing.T) {
	// This test simply ensures the interface is usable as expected
	var provider WatchlistProvider = &MockProvider{
		tracks: []map[string]string{
			{"artist": "Test Artist", "title": "Test Track"},
		},
		snapID: "test-snap",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), &database.Watchlist{})
	assert.NoError(t, err)
	assert.Equal(t, "test-snap", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
}

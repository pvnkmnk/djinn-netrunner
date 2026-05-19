package services

import (
	"context"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/interfaces"
	"github.com/pvnkmnk/netrunner/backend/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestProviderInterface(t *testing.T) {
	// This test simply ensures the interface is usable as expected
	var provider interfaces.WatchlistProvider = &testutil.MockProvider{
		Tracks: []map[string]string{
			{"artist": "Test Artist", "title": "Test Track"},
		},
		SnapID: "test-snap",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), &database.Watchlist{})
	assert.NoError(t, err)
	assert.Equal(t, "test-snap", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
}

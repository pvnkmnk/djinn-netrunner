package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSpotifySourceType(t *testing.T) {
	tests := []struct {
		name       string
		sourceType string
		want       bool
	}{
		{"spotify_playlist", "spotify_playlist", true},
		{"spotify_liked", "spotify_liked", true},
		{"spotify_discover", "spotify_discover", true},
		{"rss", "rss", false},
		{"lastfm", "lastfm", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSpotifySourceType(tt.sourceType)
			if got != tt.want {
				t.Errorf("isSpotifySourceType(%q) = %v, want %v", tt.sourceType, got, tt.want)
			}
		})
	}
}

// linkStubProvider stands in for a feed provider: one track carries a page URL
// (as RSSProvider does for every item) and one does not.
type linkStubProvider struct{}

func (linkStubProvider) FetchTracks(ctx context.Context, _ *database.Watchlist) ([]map[string]string, string, error) {
	return []map[string]string{
		{"artist": "PUP", "title": "Lionheart", "album": "PUP", "source_link": "https://example.invalid/album/pup"},
		{"artist": "PUP", "title": "Guilt Trip"},
	}, "snap-1", nil
}

func (linkStubProvider) ValidateConfig(string) error { return nil }

// A feed-linked watchlist is the production writer for jobitems.source_url. If
// the sync handler drops the link, the yt-dlp fallback silently becomes
// unreachable again (zero items carry a source_url), which is the defect this
// guards: the second entrance into the import stage must stay reachable.
func TestSyncHandlerCarriesProviderSourceURL(t *testing.T) {
	db := setupTestDB(t)
	watchlistSvc := NewWatchlistService(db, &MockSpotifyAuth{}, &config.Config{})
	watchlistSvc.RegisterProvider("link_stub", linkStubProvider{})

	wl, err := watchlistSvc.CreateWatchlist("Feed", "link_stub", "https://example.invalid/feed.xml", uuid.Nil, nil)
	require.NoError(t, err)

	job := database.Job{
		Type:        "sync",
		State:       "running",
		ScopeType:   "watchlist",
		ScopeID:     wl.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "test",
	}
	require.NoError(t, db.Create(&job).Error)

	handler := NewSyncHandler(db, nil, watchlistSvc)
	require.NoError(t, handler.Execute(context.Background(), job.ID, job))

	var items []database.JobItem
	require.NoError(t, db.Order("sequence ASC").Find(&items).Error)
	require.Len(t, items, 2)

	assert.Equal(t, "https://example.invalid/album/pup", items[0].SourceURL,
		"the provider's page URL must reach the item or the fallback entrance is unreachable")
	assert.Empty(t, items[1].SourceURL, "a track with no provider link must not invent one")
}

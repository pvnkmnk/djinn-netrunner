package services

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/zmb3/spotify/v2"
	"gorm.io/gorm"
)

type MockSpotifyAuth struct {
	client *spotify.Client
}

func (m *MockSpotifyAuth) GetClient(ctx context.Context, userID uint64) (*spotify.Client, error) {
	return m.client, nil
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	err = database.Migrate(db)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	return db
}

func TestWatchlistService(t *testing.T) {
	db := setupTestDB(t)
	auth := &MockSpotifyAuth{}
	cfg := &config.Config{LastFMApiKey: "test-key"}
	service := NewWatchlistService(db, auth, cfg)

	// Create a profile first
	profile := database.QualityProfile{
		Name:           "Test Profile",
		PreferLossless: true,
	}
	db.Create(&profile)

	t.Run("Create Watchlist", func(t *testing.T) {
		w, err := service.CreateWatchlist("My Playlist", "spotify_playlist", "spotify:playlist:123", profile.ID, nil)
		assert.NoError(t, err)
		assert.Equal(t, "My Playlist", w.Name)
		assert.Equal(t, profile.ID, w.QualityProfileID)
	})

	t.Run("Create Duplicate Fail", func(t *testing.T) {
		_, err := service.CreateWatchlist("My Playlist 2", "spotify_playlist", "spotify:playlist:123", profile.ID, nil)
		assert.Error(t, err)
	})

	t.Run("Get Watchlists", func(t *testing.T) {
		lists, err := service.GetWatchlists(0, "admin")
		assert.NoError(t, err)
		assert.Len(t, lists, 1)
		assert.Equal(t, "My Playlist", lists[0].Name)
		assert.NotNil(t, lists[0].QualityProfile)
	})

	t.Run("Update Sync Status", func(t *testing.T) {
		lists, _ := service.GetWatchlists(0, "admin")
		id := lists[0].ID
		err := service.UpdateLastSynced(id, "new-snapshot-id")
		assert.NoError(t, err)

		updated, _ := service.GetWatchlist(id)
		assert.Equal(t, "new-snapshot-id", updated.LastSnapshotID)
		assert.NotNil(t, updated.LastSyncedAt)
	})

	t.Run("Delete Watchlist", func(t *testing.T) {
		lists, _ := service.GetWatchlists(0, "admin")
		id := lists[0].ID
		err := service.DeleteWatchlist(id)
		assert.NoError(t, err)

		lists, _ = service.GetWatchlists(0, "admin")
		assert.Len(t, lists, 0)
	})

	t.Run("Register and Fetch from Mock Provider", func(t *testing.T) {
		mock := &testutil.MockProvider{
			Tracks: []map[string]string{
				{"artist": "Mock Artist", "title": "Mock Track"},
			},
			SnapID: "mock-snap",
		}
		service.RegisterProvider("mock_source", mock)

		watchlist := &database.Watchlist{
			SourceType: "mock_source",
			SourceURI:  "mock:uri",
		}

		tracks, snap, err := service.FetchWatchlistTracks(context.Background(), watchlist)
		assert.NoError(t, err)
		assert.Equal(t, "mock-snap", snap)
		assert.Len(t, tracks, 1)
		assert.Equal(t, "Mock Artist", tracks[0]["artist"])
	})
}

func TestWatchlistServiceRegistersTenProviderSources(t *testing.T) {
	db := setupTestDB(t)
	service := NewWatchlistService(db, nil, &config.Config{})

	expectedSources := []string{
		"spotify_playlist",
		"spotify_liked",
		"spotify_discover",
		"lastfm_loved",
		"lastfm_top",
		"listenbrainz_listens",
		"discogs_wantlist",
		"rss_feed",
		"local_file",
		"local_directory",
	}

	for _, source := range expectedSources {
		if _, ok := service.providers[source]; !ok {
			t.Fatalf("expected provider source %q to be registered", source)
		}
	}
	if len(service.providers) != len(expectedSources) {
		t.Fatalf("expected %d provider sources, got %d", len(expectedSources), len(service.providers))
	}
}

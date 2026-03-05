package services

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

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
	service := NewWatchlistService(db)

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
		lists, err := service.GetWatchlists()
		assert.NoError(t, err)
		assert.Len(t, lists, 1)
		assert.Equal(t, "My Playlist", lists[0].Name)
		assert.NotNil(t, lists[0].QualityProfile)
	})

	t.Run("Update Sync Status", func(t *testing.T) {
		lists, _ := service.GetWatchlists()
		id := lists[0].ID
		err := service.UpdateLastSynced(id, "new-snapshot-id")
		assert.NoError(t, err)

		updated, _ := service.GetWatchlist(id)
		assert.Equal(t, "new-snapshot-id", updated.LastSnapshotID)
		assert.NotNil(t, updated.LastSyncedAt)
	})

	t.Run("Delete Watchlist", func(t *testing.T) {
		lists, _ := service.GetWatchlists()
		id := lists[0].ID
		err := service.DeleteWatchlist(id)
		assert.NoError(t, err)

		lists, _ = service.GetWatchlists()
		assert.Len(t, lists, 0)
	})
}

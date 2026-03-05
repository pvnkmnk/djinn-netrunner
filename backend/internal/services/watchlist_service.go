package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/zmb3/spotify/v2"
	"gorm.io/gorm"
)

// WatchlistService manages Spotify watchlists
type WatchlistService struct {
	db          *gorm.DB
	spotifyAuth *api.SpotifyAuthHandler
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(db *gorm.DB, spotifyAuth *api.SpotifyAuthHandler) *WatchlistService {
	return &WatchlistService{
		db:          db,
		spotifyAuth: spotifyAuth,
	}
}

// FetchWatchlistTracks retrieves tracks from a Spotify source (Playlist or Liked Songs)
func (s *WatchlistService) FetchWatchlistTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.OwnerUserID == nil {
		return nil, "", errors.New("watchlist has no owner user")
	}

	client, err := s.spotifyAuth.GetClient(ctx, *watchlist.OwnerUserID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get spotify client: %w", err)
	}

	var allTracks []map[string]string
	var snapshotID string

	if watchlist.SourceType == "spotify_liked" {
		// Fetch Liked Songs (Saved Tracks)
		limit := 50
		offset := 0
		for {
			page, err := client.CurrentUsersTracks(ctx, spotify.Limit(limit), spotify.Offset(offset))
			if err != nil {
				return nil, "", err
			}

			for _, item := range page.Tracks {
				artistName := ""
				if len(item.Artists) > 0 {
					artistName = item.Artists[0].Name
				}
				allTracks = append(allTracks, map[string]string{
					"id":     string(item.ID),
					"artist": artistName,
					"title":  item.Name,
					"album":  item.Album.Name,
				})
			}

			offset += len(page.Tracks)
			if offset >= int(page.Total) {
				break
			}
		}
		// Liked songs don't have a snapshot ID in the same way, we use total count or timestamp
		snapshotID = fmt.Sprintf("liked:%d", len(allTracks))

	} else if watchlist.SourceType == "spotify_playlist" {
		// Fetch Playlist tracks
		playlistID := s.ExtractPlaylistID(watchlist.SourceURI)
		id := spotify.ID(playlistID)

		// Get playlist metadata for snapshot ID
		playlist, err := client.GetPlaylist(ctx, id, spotify.Fields("snapshot_id"))
		if err != nil {
			return nil, "", err
		}
		snapshotID = playlist.SnapshotID

		limit := 100
		offset := 0
		for {
			page, err := client.GetPlaylistItems(ctx, id, spotify.Limit(limit), spotify.Offset(offset))
			if err != nil {
				return nil, "", err
			}

			for _, item := range page.Items {
				if item.Track.Track == nil {
					continue
				}
				t := item.Track.Track
				artistName := ""
				if len(t.Artists) > 0 {
					artistName = t.Artists[0].Name
				}
				allTracks = append(allTracks, map[string]string{
					"id":     string(t.ID),
					"artist": artistName,
					"title":  t.Name,
					"album":  t.Album.Name,
				})
			}

			offset += len(page.Items)
			if offset >= int(page.Total) {
				break
			}
		}
	} else {
		return nil, "", fmt.Errorf("unsupported source type: %s", watchlist.SourceType)
	}

	return allTracks, snapshotID, nil
}

// ExtractPlaylistID extracts the ID from a Spotify URI or URL
func (s *WatchlistService) ExtractPlaylistID(uri string) string {
	if strings.HasPrefix(uri, "spotify:playlist:") {
		return strings.TrimPrefix(uri, "spotify:playlist:")
	}
	if strings.Contains(uri, "open.spotify.com/playlist/") {
		parts := strings.Split(uri, "/playlist/")
		if len(parts) > 1 {
			id := strings.Split(parts[1], "?")[0]
			return strings.Split(id, "#")[0]
		}
	}
	return uri
}

// CreateWatchlist adds a new watchlist to the database
func (s *WatchlistService) CreateWatchlist(name, sourceType, uri string, profileID uuid.UUID, userID *uint64) (*database.Watchlist, error) {
	// Check if already exists
	var existing database.Watchlist
	err := s.db.Where("source_uri = ?", uri).First(&existing).Error
	if err == nil {
		return nil, errors.New("watchlist already exists for this URI")
	}

	watchlist := database.Watchlist{
		Name:             name,
		SourceType:       sourceType,
		SourceURI:        uri,
		QualityProfileID: profileID,
		Enabled:          true,
		OwnerUserID:      userID,
	}

	if err := s.db.Create(&watchlist).Error; err != nil {
		return nil, err
	}

	// Preload the profile for convenience
	s.db.Preload("QualityProfile").First(&watchlist)

	return &watchlist, nil
}

// GetWatchlists retrieves all enabled watchlists
func (s *WatchlistService) GetWatchlists() ([]database.Watchlist, error) {
	var watchlists []database.Watchlist
	err := s.db.Preload("QualityProfile").Find(&watchlists).Error
	return watchlists, err
}

// GetWatchlist retrieves a single watchlist by ID
func (s *WatchlistService) GetWatchlist(id uuid.UUID) (*database.Watchlist, error) {
	var watchlist database.Watchlist
	err := s.db.Preload("QualityProfile").First(&watchlist, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &watchlist, nil
}

// UpdateWatchlistStatus enables or disables a watchlist
func (s *WatchlistService) UpdateWatchlistStatus(id uuid.UUID, enabled bool) error {
	return s.db.Model(&database.Watchlist{}).Where("id = ?", id).Update("enabled", enabled).Error
}

// DeleteWatchlist removes a watchlist
func (s *WatchlistService) DeleteWatchlist(id uuid.UUID) error {
	return s.db.Delete(&database.Watchlist{}, "id = ?", id).Error
}

// GetNewTracks compares current tracks with last known snapshot and returns new additions
func (s *WatchlistService) GetNewTracks(ctx context.Context, watchlist *database.Watchlist, currentTracks []map[string]string) []map[string]string {
	if watchlist.LastSnapshotID == "" {
		// First sync, all tracks are "new"
		return currentTracks
	}

	// For a more robust implementation, we would store the previous track IDs in the DB
	// or in a cache. Given our architecture, we'll use a simple approach:
	// We'll fetch the tracks already acquired for this watchlist scope from the acquisitions table.
	
	var acquired []database.Acquisition
	s.db.Where("owner_user_id = ?", watchlist.OwnerUserID).Find(&acquired)
	
	existingMap := make(map[string]bool)
	for _, a := range acquired {
		// Create a unique key for comparison (Artist - Title)
		key := strings.ToLower(fmt.Sprintf("%s-%s", a.Artist, a.TrackTitle))
		existingMap[key] = true
	}

	var newTracks []map[string]string
	for _, t := range currentTracks {
		key := strings.ToLower(fmt.Sprintf("%s-%s", t["artist"], t["title"]))
		if !existingMap[key] {
			newTracks = append(newTracks, t)
		}
	}

	return newTracks
}

// UpdateLastSynced updates the last synced timestamp and snapshot ID
func (s *WatchlistService) UpdateLastSynced(id uuid.UUID, snapshotID string) error {
	now := time.Now()
	return s.db.Model(&database.Watchlist{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_synced_at":   &now,
		"last_snapshot_id": snapshotID,
	}).Error
}

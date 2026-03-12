package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// WatchlistService manages music watchlists across different providers
type WatchlistService struct {
	db          *gorm.DB
	spotifyAuth *api.SpotifyAuthHandler
	providers   map[string]WatchlistProvider
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(db *gorm.DB, spotifyAuth *api.SpotifyAuthHandler, cfg *config.Config) *WatchlistService {
	s := &WatchlistService{
		db:          db,
		spotifyAuth: spotifyAuth,
		providers:   make(map[string]WatchlistProvider),
	}

	// Register default providers
	s.RegisterProvider("spotify_liked", NewSpotifyProvider(spotifyAuth))
	s.RegisterProvider("spotify_playlist", NewSpotifyProvider(spotifyAuth))
	s.RegisterProvider("lastfm_loved", NewLastFMProvider(cfg.LastFMApiKey))
	s.RegisterProvider("lastfm_top", NewLastFMProvider(cfg.LastFMApiKey))
	s.RegisterProvider("listenbrainz_listens", NewListenBrainzProvider(cfg.ListenBrainzToken))
	s.RegisterProvider("rss_feed", NewRSSProvider())
	s.RegisterProvider("discogs_wantlist", NewDiscogsProvider(cfg.DiscogsToken))
	s.RegisterProvider("local_file", NewFileWatchlistProvider())

	return s
}

// RegisterProvider registers a new watchlist provider handler
func (s *WatchlistService) RegisterProvider(sourceType string, provider WatchlistProvider) {
	s.providers[sourceType] = provider
}

// FetchWatchlistTracks retrieves tracks from a source
func (s *WatchlistService) FetchWatchlistTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	// Check for registered providers
	if provider, ok := s.providers[watchlist.SourceType]; ok {
		return provider.FetchTracks(ctx, watchlist)
	}

	return nil, "", fmt.Errorf("unsupported source type: %s", watchlist.SourceType)
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

// FilterExistingTracks removes tracks that are already in the library or active queue
func (s *WatchlistService) FilterExistingTracks(ctx context.Context, tracks []map[string]string) []map[string]string {
	var filtered []map[string]string

	for _, t := range tracks {
		artist := t["artist"]
		title := t["title"]

		// 1. Check Library
		var count int64
		s.db.Model(&database.Track{}).Where("LOWER(artist) = LOWER(?) AND LOWER(title) = LOWER(?)", artist, title).Count(&count)
		if count > 0 {
			continue
		}

		// 2. Check active JobItems (not failed)
		s.db.Model(&database.JobItem{}).Where("LOWER(artist) = LOWER(?) AND LOWER(track_title) = LOWER(?) AND status != 'failed'", artist, title).Count(&count)
		if count > 0 {
			continue
		}

		filtered = append(filtered, t)
	}

	return filtered
}

// UpdateLastSynced updates the last synced timestamp and snapshot ID
func (s *WatchlistService) UpdateLastSynced(id uuid.UUID, snapshotID string) error {
	now := time.Now()
	return s.db.Model(&database.Watchlist{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_synced_at":   &now,
		"last_snapshot_id": snapshotID,
	}).Error
}

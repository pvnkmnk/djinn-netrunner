package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/interfaces"
	"gorm.io/gorm"
)

// WatchlistService manages music watchlists across different providers
type WatchlistService struct {
	db          *gorm.DB
	spotifyAuth interfaces.SpotifyClientProvider
	providers   map[string]interfaces.WatchlistProvider
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(db *gorm.DB, spotifyAuth interfaces.SpotifyClientProvider, cfg *config.Config) *WatchlistService {
	s := &WatchlistService{
		db:          db,
		spotifyAuth: spotifyAuth,
		providers:   make(map[string]interfaces.WatchlistProvider),
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
	s.RegisterProvider("local_directory", NewDirectoryWatchlistProvider())

	return s
}

// RegisterProvider registers a new watchlist provider handler
func (s *WatchlistService) RegisterProvider(sourceType string, provider interfaces.WatchlistProvider) {
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

// ValidateWatchlist validates a watchlist's configuration
func (s *WatchlistService) ValidateWatchlist(watchlist *database.Watchlist) error {
	provider, ok := s.providers[watchlist.SourceType]
	if !ok {
		return fmt.Errorf("unsupported source type: %s", watchlist.SourceType)
	}

	// Use SourceURI as the primary config identifier
	return provider.ValidateConfig(watchlist.SourceURI)
}

// CreateWatchlist adds a new watchlist to the database
func (s *WatchlistService) CreateWatchlist(name, sourceType, uri string, profileID uuid.UUID, userID *uint64) (*database.Watchlist, error) {
	watchlist := database.Watchlist{
		Name:             name,
		SourceType:       sourceType,
		SourceURI:        uri,
		QualityProfileID: profileID,
		Enabled:          true,
		OwnerUserID:      userID,
	}

	// Validate configuration/URI
	if err := s.ValidateWatchlist(&watchlist); err != nil {
		return nil, err
	}

	// Check if already exists
	var existing database.Watchlist
	err := s.db.Where("source_uri = ?", uri).First(&existing).Error
	if err == nil {
		return nil, errors.New("watchlist already exists for this URI")
	}

	if err := s.db.Create(&watchlist).Error; err != nil {
		return nil, err
	}

	// Preload the profile for convenience
	s.db.Preload("QualityProfile").First(&watchlist)

	return &watchlist, nil
}

// GetWatchlists retrieves all enabled watchlists, filtering by owner if role is not "admin"
func (s *WatchlistService) GetWatchlists(userID uint64, role string) ([]database.Watchlist, error) {
	var watchlists []database.Watchlist
	query := s.db.Preload("QualityProfile")
	if role != "admin" {
		query = query.Where("owner_user_id = ?", userID)
	}
	err := query.Find(&watchlists).Error
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

// makeTrackKey creates a unique key for track identification
func makeTrackKey(artist, title string) string {
	return strings.ToLower(fmt.Sprintf("%s-%s", artist, title))
}

// collectUniqueArtists extracts unique artist names from tracks
func collectUniqueArtists(tracks []map[string]string) []string {
	artistMap := make(map[string]bool)
	for _, t := range tracks {
		if a, ok := t["artist"]; ok && a != "" {
			artistMap[strings.ToLower(a)] = true
		}
	}
	var artists []string
	for a := range artistMap {
		artists = append(artists, a)
	}
	return artists
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

	// Bolt Optimization: Filter acquisitions by artists in the current batch to avoid loading entire history.
	artists := collectUniqueArtists(currentTracks)
	if len(artists) == 0 {
		return []map[string]string{}
	}

	var acquired []database.Acquisition
	// Bolt Optimization: Select only necessary columns to reduce memory allocation
	if err := s.db.Select("artist, track_title").Where("owner_user_id = ? AND LOWER(artist) IN ?", watchlist.OwnerUserID, artists).Find(&acquired).Error; err != nil {
		// Returning empty slice to prevent creating duplicate jobs on DB error.
		// Consider changing the function signature to return an error for better handling.
		return []map[string]string{}
	}

	existingMap := make(map[string]bool)
	for _, a := range acquired {
		// Create a unique key for comparison (Artist - Title)
		key := makeTrackKey(a.Artist, a.TrackTitle)
		existingMap[key] = true
	}

	var newTracks []map[string]string
	for _, t := range currentTracks {
		key := makeTrackKey(t["artist"], t["title"])
		if !existingMap[key] {
			newTracks = append(newTracks, t)
		}
	}

	return newTracks
}

// FilterExistingTracks removes tracks that are already in the library or active queue
func (s *WatchlistService) FilterExistingTracks(ctx context.Context, tracks []map[string]string) []map[string]string {
	if len(tracks) == 0 {
		return tracks
	}

	// Bolt Optimization: Replace N+1 queries with bulk-fetch and hash-map lookup.
	// This reduces database roundtrips from 2N to 2.

	// 1. Collect unique artists using helper function
	artists := collectUniqueArtists(tracks)
	if len(artists) == 0 {
		return tracks
	}

	// 2. Bulk fetch existing tracks from library
	var existingTracks []database.Track
	// Bolt Optimization: Select only necessary columns to reduce memory allocation
	if err := s.db.Select("artist, title").Where("LOWER(artist) IN ?", artists).Find(&existingTracks).Error; err != nil {
		// Returning empty slice to prevent creating duplicate jobs on DB error.
		return []map[string]string{}
	}

	existingMap := make(map[string]bool)
	for _, et := range existingTracks {
		key := makeTrackKey(et.Artist, et.Title)
		existingMap[key] = true
	}

	// 3. Bulk fetch active job items
	var activeItems []database.JobItem
	// Bolt Optimization: Select only necessary columns to reduce memory allocation
	if err := s.db.Select("artist, track_title").Where("LOWER(artist) IN ? AND status != 'failed'", artists).Find(&activeItems).Error; err != nil {
		// Returning empty slice to prevent creating duplicate jobs on DB error.
		return []map[string]string{}
	}

	for _, ai := range activeItems {
		key := makeTrackKey(ai.Artist, ai.TrackTitle)
		existingMap[key] = true
	}

	// 4. Filter tracks
	var filtered []map[string]string
	for _, t := range tracks {
		key := makeTrackKey(t["artist"], t["title"])
		if !existingMap[key] {
			filtered = append(filtered, t)
		}
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

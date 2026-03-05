package services

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// WatchlistService manages Spotify watchlists
type WatchlistService struct {
	db *gorm.DB
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(db *gorm.DB) *WatchlistService {
	return &WatchlistService{
		db: db,
	}
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

// UpdateLastSynced updates the last synced timestamp and snapshot ID
func (s *WatchlistService) UpdateLastSynced(id uuid.UUID, snapshotID string) error {
	now := time.Now()
	return s.db.Model(&database.Watchlist{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_synced_at":   &now,
		"last_snapshot_id": snapshotID,
	}).Error
}

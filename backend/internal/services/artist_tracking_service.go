package services

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// ArtistTrackingService manages monitored artists and their releases
type ArtistTrackingService struct {
	db *gorm.DB
	mb *MusicBrainzService
}

// NewArtistTrackingService creates a new artist tracking service
func NewArtistTrackingService(db *gorm.DB, mb *MusicBrainzService) *ArtistTrackingService {
	return &ArtistTrackingService{
		db: db,
		mb: mb,
	}
}

// AddMonitoredArtist adds a new artist to the system and starts monitoring
func (s *ArtistTrackingService) AddMonitoredArtist(mbid string, qualityProfileID uuid.UUID) (*database.MonitoredArtist, error) {
	// 1. Fetch artist details from MusicBrainz to ensure it exists and get metadata
	// (Simplified for now, in a real implementation we'd parse the MB response properly)
	
	// Check if artist already exists
	var existing database.MonitoredArtist
	err := s.db.Where("music_brainz_id = ?", mbid).First(&existing).Error
	if err == nil {
		return nil, errors.New("artist already monitored")
	}

	// Create artist record
	// For now, we'll assume the name is provided or we fetch it
	artist := database.MonitoredArtist{
		MusicBrainzID:    mbid,
		QualityProfileID: qualityProfileID,
		Monitored:        true,
		MonitorNew:       true,
		MonitorAlbums:    true,
		MonitorEPs:       true,
	}

	if err := s.db.Create(&artist).Error; err != nil {
		return nil, err
	}

	return &artist, nil
}

// GetMonitoredArtists retrieves all artists with monitoring enabled
func (s *ArtistTrackingService) GetMonitoredArtists() ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	err := s.db.Preload("QualityProfile").Find(&artists, "monitored = ?", true).Error
	return artists, err
}

// UpdateArtistStatus updates the status of an artist
func (s *ArtistTrackingService) UpdateArtistStatus(id uuid.UUID, monitored bool) error {
	return s.db.Model(&database.MonitoredArtist{}).Where("id = ?", id).Update("monitored", monitored).Error
}

// SyncDiscography fetches the latest releases for an artist and updates tracked releases
func (s *ArtistTrackingService) SyncDiscography(artistID uuid.UUID) error {
	var artist database.MonitoredArtist
	if err := s.db.First(&artist, "id = ?", artistID).Error; err != nil {
		return err
	}

	// Fetch from MusicBrainz
	data, err := s.mb.GetArtistDiscography(artist.MusicBrainzID)
	if err != nil {
		return err
	}

	// Parse release groups and upsert into TrackedRelease table
	releaseGroups, ok := data["release-groups"].([]interface{})
	if !ok {
		return nil
	}

	// PERFORMANCE: Pre-fetch existing releases to avoid N+1 query problem
	var existingReleases []database.TrackedRelease
	if err := s.db.Where("artist_id = ?", artist.ID).Find(&existingReleases).Error; err != nil {
		return err
	}

	existingMap := make(map[string]database.TrackedRelease)
	for _, r := range existingReleases {
		existingMap[r.ReleaseGroupID] = r
	}

	var newReleases []database.TrackedRelease
	now := time.Now()

	for _, rg := range releaseGroups {
		group := rg.(map[string]interface{})
		rgID := group["id"].(string)
		title := group["title"].(string)
		primaryType := ""
		if t, ok := group["primary-type"].(string); ok {
			primaryType = t
		}

		// Basic filtering based on artist preferences
		shouldMonitor := false
		switch primaryType {
		case "Album":
			shouldMonitor = artist.MonitorAlbums
		case "EP":
			shouldMonitor = artist.MonitorEPs
		case "Single":
			shouldMonitor = artist.MonitorSingles
		}

		if existing, ok := existingMap[rgID]; !ok {
			// Collect new releases for batch insertion
			newReleases = append(newReleases, database.TrackedRelease{
				ArtistID:       artist.ID,
				ReleaseGroupID: rgID,
				Title:          title,
				ReleaseType:    primaryType,
				Status:         "wanted",
				Monitored:      shouldMonitor,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		} else if existing.Title != title {
			// Only update if title changed to minimize DB writes
			s.db.Model(&existing).Updates(map[string]interface{}{
				"title":      title,
				"updated_at": now,
			})
		}
	}

	// PERFORMANCE: Use batch insertion for new records
	if len(newReleases) > 0 {
		if err := s.db.CreateInBatches(newReleases, 100).Error; err != nil {
			return err
		}
	}

	// Update last scan date
	scanNow := time.Now()
	return s.db.Model(&artist).Update("last_scan_date", &scanNow).Error
}

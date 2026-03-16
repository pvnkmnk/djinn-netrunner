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

	// Pre-fetch existing releases to avoid N+1 query problem
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
		rgID, _ := group["id"].(string)
		title, _ := group["title"].(string)
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
		case "Compilation":
			shouldMonitor = artist.MonitorCompilations
		case "Live":
			shouldMonitor = artist.MonitorLive
		}

		if release, exists := existingMap[rgID]; !exists {
			// Prepare for batch creation
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
		} else {
			// Update existing only if title changed to minimize writes
			if release.Title != title {
				s.db.Model(&release).Updates(map[string]interface{}{
					"title":      title,
					"updated_at": now,
				})
			}
		}
	}

	// Batch create new releases
	if len(newReleases) > 0 {
		if err := s.db.CreateInBatches(newReleases, 100).Error; err != nil {
			return err
		}
	}

	// Update last scan date
	return s.db.Model(&artist).Update("last_scan_date", &now).Error
}

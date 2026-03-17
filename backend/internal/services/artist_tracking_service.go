package services

import (
	"errors"
	"log"
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
func (s *ArtistTrackingService) AddMonitoredArtist(mbid string, qualityProfileID uuid.UUID, name, sortName string) (*database.MonitoredArtist, error) {
	// 1. Fetch artist details from MusicBrainz to ensure it exists and get metadata
	// (Simplified for now, in a real implementation we'd parse the MB response properly)

	// Check if artist already exists
	var existing database.MonitoredArtist
	err := s.db.Where("music_brainz_id = ?", mbid).First(&existing).Error
	if err == nil {
		return nil, errors.New("artist already monitored")
	}

	// Use provided name or fallback to MBID
	artistName := name
	if artistName == "" {
		artistName = mbid
	}

	// Create artist record
	artist := database.MonitoredArtist{
		MusicBrainzID:    mbid,
		Name:             artistName,
		SortName:         sortName,
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

// DeleteMonitoredArtist removes an artist from monitoring
func (s *ArtistTrackingService) DeleteMonitoredArtist(id uuid.UUID) error {
	return s.db.Delete(&database.MonitoredArtist{}, "id = ?", id).Error
}

// SyncDiscography fetches the latest releases for an artist and creates acquisition jobs for new releases
func (s *ArtistTrackingService) SyncDiscography(artistID uuid.UUID) error {
	var artist database.MonitoredArtist
	if err := s.db.Preload("QualityProfile").First(&artist, "id = ?", artistID).Error; err != nil {
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

	var newReleases []database.TrackedRelease

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

		// Upsert TrackedRelease
		var release database.TrackedRelease
		err := s.db.Where("artist_id = ? AND release_group_id = ?", artist.ID, rgID).First(&release).Error

		now := time.Now()
		if err != nil {
			// Create new
			release = database.TrackedRelease{
				ArtistID:       artist.ID,
				ReleaseGroupID: rgID,
				Title:          title,
				ReleaseType:    primaryType,
				Status:         "wanted",
				Monitored:      shouldMonitor,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			s.db.Create(&release)
			if shouldMonitor {
				newReleases = append(newReleases, release)
			}
		} else {
			// Update existing if needed
			s.db.Model(&release).Updates(map[string]interface{}{
				"title":      title,
				"updated_at": now,
			})
		}
	}

	// Create acquisition job for new releases
	if len(newReleases) > 0 {
		job := database.Job{
			Type:        "acquisition",
			State:       "queued",
			ScopeType:   "artist",
			ScopeID:     artistID.String(),
			OwnerUserID: artist.OwnerUserID,
			CreatedBy:   "artist_tracking",
		}

		if err := s.db.Create(&job).Error; err != nil {
			log.Printf("[ARTIST] Error creating acquisition job: %v", err)
		} else {
			// Create job items for each new release
			for i, rel := range newReleases {
				item := database.JobItem{
					JobID:           job.ID,
					Sequence:        i,
					NormalizedQuery: rel.Title,
					Artist:          artist.Name,
					Album:           rel.Title,
					TrackTitle:      rel.Title,
					Status:          "queued",
					OwnerUserID:     artist.OwnerUserID,
				}
				s.db.Create(&item)

				// Mark release as queued
				s.db.Model(&rel).Update("status", "queued")
			}
			log.Printf("[ARTIST] Created acquisition job %d with %d items for artist %s", job.ID, len(newReleases), artist.Name)
		}
	}

	// Update last scan date
	now := time.Now()
	return s.db.Model(&artist).Update("last_scan_date", &now).Error
}

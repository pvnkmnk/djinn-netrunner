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

	// Bolt Optimization: Bulk fetch existing releases to avoid N+1 queries in the loop
	var existingReleases []database.TrackedRelease
	if err := s.db.Where("artist_id = ?", artist.ID).Find(&existingReleases).Error; err != nil {
		return err
	}

	existingMap := make(map[string]database.TrackedRelease)
	for _, er := range existingReleases {
		existingMap[er.ReleaseGroupID] = er
	}

	var releasesToCreate []database.TrackedRelease
	var releasesToUpdate []database.TrackedRelease
	var newReleasesForJob []database.TrackedRelease

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
		}

		if release, exists := existingMap[rgID]; !exists {
			// Create new
			// Bolt Fix: Manually generate ID to ensure consistency between creation and job item reference
			newRel := database.TrackedRelease{
				ID:             uuid.New(),
				ArtistID:       artist.ID,
				ReleaseGroupID: rgID,
				Title:          title,
				ReleaseType:    primaryType,
				Status:         "wanted",
				Monitored:      shouldMonitor,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			releasesToCreate = append(releasesToCreate, newRel)
			if shouldMonitor {
				newReleasesForJob = append(newReleasesForJob, newRel)
			}
		} else {
			// Update existing if title changed
			if release.Title != title {
				release.Title = title
				release.UpdatedAt = now
				releasesToUpdate = append(releasesToUpdate, release)
			}
		}
	}

	// Bolt Optimization: Use CreateInBatches for bulk inserts
	if len(releasesToCreate) > 0 {
		if err := s.db.CreateInBatches(releasesToCreate, 100).Error; err != nil {
			log.Printf("[ARTIST] Error batch creating releases: %v", err)
		}
	}

	// Individual updates for modified releases (usually rare, so N queries here is acceptable
	// but we could also optimize this if needed)
	for _, rel := range releasesToUpdate {
		s.db.Model(&rel).Updates(map[string]interface{}{
			"title":      rel.Title,
			"updated_at": rel.UpdatedAt,
		})
	}

	// Create acquisition job for new releases
	if len(newReleasesForJob) > 0 {
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
			var jobItems []database.JobItem
			var releaseIDsToMarkQueued []uuid.UUID

			// Create job items for each new release
			for i, rel := range newReleasesForJob {
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
				jobItems = append(jobItems, item)
				releaseIDsToMarkQueued = append(releaseIDsToMarkQueued, rel.ID)
			}

			// Bolt Optimization: Batch create job items
			if err := s.db.CreateInBatches(jobItems, 100).Error; err != nil {
				log.Printf("[ARTIST] Error batch creating job items: %v", err)
			}

			// Bolt Optimization: Bulk update release status
			if len(releaseIDsToMarkQueued) > 0 {
				s.db.Model(&database.TrackedRelease{}).
					Where("id IN ?", releaseIDsToMarkQueued).
					Update("status", "queued")
			}

			log.Printf("[ARTIST] Created acquisition job %d with %d items for artist %s", job.ID, len(newReleasesForJob), artist.Name)
		}
	}

	// Update last scan date
	scanNow := time.Now()
	return s.db.Model(&artist).Update("last_scan_date", &scanNow).Error
}

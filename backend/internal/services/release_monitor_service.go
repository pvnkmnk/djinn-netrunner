package services

import (
	"fmt"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// ReleaseMonitorService handles the background task of checking for new releases
type ReleaseMonitorService struct {
	db *gorm.DB
	at *ArtistTrackingService
}

// NewReleaseMonitorService creates a new release monitor service
func NewReleaseMonitorService(db *gorm.DB, at *ArtistTrackingService) *ReleaseMonitorService {
	return &ReleaseMonitorService{
		db: db,
		at: at,
	}
}

// CheckAllArtists checks all monitored artists for new releases
func (s *ReleaseMonitorService) CheckAllArtists() error {
	var artists []database.MonitoredArtist
	// Find artists that haven't been checked in the last 24 hours
	cutoff := time.Now().Add(-24 * time.Hour)
	err := s.db.Where("monitored = ? AND (last_release_check IS NULL OR last_release_check < ?)", true, cutoff).Find(&artists).Error
	if err != nil {
		return err
	}

	fmt.Printf("[MONITOR] Checking %d artists for new releases\n", len(artists))

	for _, artist := range artists {
		fmt.Printf("[MONITOR] Checking artist: %s\n", artist.Name)
		if err := s.at.SyncDiscography(artist.ID); err != nil {
			fmt.Printf("[MONITOR] Error checking artist %s: %v\n", artist.Name, err)
			continue
		}

		// Update last release check
		now := time.Now()
		s.db.Model(&artist).Update("last_release_check", &now)
		
		// Respect MusicBrainz rate limit (SyncDiscography already does, but extra safety)
		time.Sleep(2 * time.Second)
	}

	return nil
}

// StartBackgroundTask starts a loop that runs every hour to check for new releases
func (s *ReleaseMonitorService) StartBackgroundTask() {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			if err := s.CheckAllArtists(); err != nil {
				fmt.Printf("[MONITOR] Background check failed: %v\n", err)
			}
		}
	}()
}

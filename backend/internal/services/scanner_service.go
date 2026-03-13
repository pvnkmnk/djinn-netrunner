package services

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type ScannerService struct {
	db       *gorm.DB
	metadata *MetadataExtractor
}

func NewScannerService(db *gorm.DB) *ScannerService {
	return &ScannerService{
		db:       db,
		metadata: NewMetadataExtractor(),
	}
}

type ScanJob struct {
	Path      string
	LibraryID uuid.UUID
}

func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID uuid.UUID, path string) error {
	log.Printf("[SCANNER] Starting scan of %s", path)

	// 1. Worker Pool Setup
	numWorkers := 4
	jobs := make(chan ScanJob, 100)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				s.processFile(job.Path, job.LibraryID)
			}
		}()
	}

	// 2. Discovery
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && s.metadata.IsAudioFile(filePath) {
			select {
			case jobs <- ScanJob{Path: filePath, LibraryID: libraryID}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	close(jobs)
	wg.Wait()

	log.Printf("[SCANNER] Finished scan of %s", path)
	return err
}

func (s *ScannerService) processFile(path string, libraryID uuid.UUID) {
	// Extract metadata
	meta, err := s.metadata.Extract(path)
	if err != nil {
		log.Printf("[SCANNER] Error extracting metadata from %s: %v", path, err)
		return
	}

	// Compute hash
	hash, _ := s.metadata.HashFile(path)

	// Create or update track
	track := database.Track{
		LibraryID: libraryID,
		Title:     meta.Title,
		Artist:    meta.Artist,
		Album:     meta.Album,
		Path:      path,
		Format:    meta.Format,
		FileSize:  meta.FileSize,
	}

	// Simple upsert based on path
	err = s.db.Where("path = ?", path).
		Assign(database.Track{
			Title:    track.Title,
			Artist:   track.Artist,
			Album:    track.Album,
			Format:   track.Format,
			FileSize: track.FileSize,
		}).
		FirstOrCreate(&track).Error

	if hash != "" {
		s.db.Model(&track).Update("file_hash", hash)
	}

	if err != nil {
		log.Printf("[SCANNER] Error saving track %s: %v", path, err)
	}
}

func (s *ScannerService) PruneTracks(ctx context.Context, libraryID uuid.UUID) error {
	log.Printf("[SCANNER] Starting prune for library %s", libraryID)

	var tracks []database.Track
	if err := s.db.Where("library_id = ?", libraryID).Find(&tracks).Error; err != nil {
		return err
	}

	count := 0
	for _, t := range tracks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if _, err := os.Stat(t.Path); os.IsNotExist(err) {
				log.Printf("[SCANNER] Pruning missing file: %s", t.Path)
				s.db.Delete(&t)
				count++
			}
		}
	}

	log.Printf("[SCANNER] Prune complete. Removed %d stale records.", count)
	return nil
}

func (s *ScannerService) GetMonitoredArtists() ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	err := s.db.Preload("QualityProfile").Find(&artists).Error
	return artists, err
}

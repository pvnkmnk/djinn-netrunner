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
	log.Printf("[SCANNER] Starting scan | library_id=%s | path=%s", libraryID, path)

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
	// Bolt Optimization: filepath.WalkDir is more efficient than filepath.Walk
	// as it avoids unnecessary Lstat calls by using os.DirEntry.
	err := filepath.WalkDir(path, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && s.metadata.IsAudioFile(filePath) {
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

	log.Printf("[SCANNER] Finished scan | library_id=%s | path=%s", libraryID, path)
	return err
}

func (s *ScannerService) processFile(path string, libraryID uuid.UUID) {
	// Extract metadata
	meta, err := s.metadata.Extract(path)
	if err != nil {
		log.Printf("[SCANNER] Error extracting metadata | library_id=%s | path=%s | error=%v", libraryID, path, err)
		return
	}

	// Compute hash
	hash, _ := s.metadata.HashFile(path)

	// Bolt Optimization: Use a single FirstOrCreate with Assign to handle both Create and Update
	// in a single database roundtrip, ensuring FileHash is always current and avoiding redundant UPDATEs.
	var track database.Track
	err = s.db.Where("path = ?", path).
		Assign(database.Track{
			LibraryID: libraryID,
			Title:     meta.Title,
			Artist:    meta.Artist,
			Album:     meta.Album,
			Format:    meta.Format,
			FileSize:  meta.FileSize,
			FileHash:  hash,
		}).
		FirstOrCreate(&track).Error

	if err != nil {
		log.Printf("[SCANNER] Error saving track | library_id=%s | path=%s | error=%v", libraryID, path, err)
	}
}

func (s *ScannerService) PruneTracks(ctx context.Context, libraryID uuid.UUID) error {
	log.Printf("[SCANNER] Starting prune | library_id=%s", libraryID)

	// Bolt Optimization: Select only necessary fields and use batch DELETE
	// to reduce memory overhead and database roundtrips.
	var tracks []struct {
		ID   uuid.UUID
		Path string
	}
	if err := s.db.Model(&database.Track{}).
		Where("library_id = ?", libraryID).
		Select("id, path").
		Find(&tracks).Error; err != nil {
		return err
	}

	var toDelete []uuid.UUID
	for _, t := range tracks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if _, err := os.Stat(t.Path); os.IsNotExist(err) {
				log.Printf("[SCANNER] Pruning missing file | library_id=%s | path=%s", libraryID, t.Path)
				toDelete = append(toDelete, t.ID)
			}
		}
	}

	if len(toDelete) > 0 {
		if err := s.db.Delete(&database.Track{}, "id IN ?", toDelete).Error; err != nil {
			return err
		}
	}

	log.Printf("[SCANNER] Prune complete | library_id=%s | removed=%d", libraryID, len(toDelete))
	return nil
}

func (s *ScannerService) GetMonitoredArtists() ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	err := s.db.Preload("QualityProfile").Find(&artists).Error
	return artists, err
}

func (s *ScannerService) GetMonitoredArtistsFiltered(userID *uint64, role string) ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	query := s.db.Preload("QualityProfile")
	if role != "admin" && userID != nil {
		query = query.Where("owner_user_id = ?", *userID)
	}
	err := query.Find(&artists).Error
	return artists, err
}

package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	slog.Info("Starting scan", "library_id", libraryID, "path", path)

	// 1. Worker Pool Setup
	numWorkers := 4
	jobs := make(chan ScanJob, 100)
	var wg sync.WaitGroup

	// Per-file failures must not be swallowed: a scan that indexes some files
	// and fails on others has to surface as a failed job, not "Completed".
	var (
		mu       sync.Mutex
		indexed  int
		failed   int
		firstErr error
	)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if err := s.processFile(job.Path, job.LibraryID); err != nil {
					slog.Error("Error indexing file", "library_id", job.LibraryID, "path", job.Path, "error", err)
					mu.Lock()
					failed++
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", job.Path, err)
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				indexed++
				mu.Unlock()
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

	slog.Info("Finished scan", "library_id", libraryID, "path", path, "indexed", indexed, "failed", failed)
	if err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("scan indexed %d file(s) but failed on %d: %w", indexed, failed, firstErr)
	}
	return nil
}

func (s *ScannerService) processFile(path string, libraryID uuid.UUID) error {
	// Extract metadata
	meta, err := s.metadata.Extract(path)
	if err != nil {
		return fmt.Errorf("extract metadata: %w", err)
	}

	// Compute hash
	hash, _ := s.metadata.HashFile(path)

	// Check if track already exists — only compute expensive fingerprint for new tracks
	// or tracks that are missing a fingerprint (e.g., added before Phase 8).
	var fingerprint string
	var existing database.Track
	err = s.db.Where("path = ?", path).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Track not found — new, fingerprint and create
		fp, _, fpErr := s.metadata.Fingerprint(path)
		if fpErr == nil {
			fingerprint = fp
		}
		// Path is NOT NULL with a unique index: leaving it empty makes the first
		// insert succeed and every later track collide on idx_tracks_path.
		track := database.Track{
			LibraryID:   libraryID,
			Title:       meta.Title,
			Artist:      meta.Artist,
			Album:       meta.Album,
			Path:        path,
			Format:      meta.Format,
			FileSize:    meta.FileSize,
			FileHash:    hash,
			Fingerprint: fingerprint,
		}
		if meta.TrackNumber > 0 {
			track.TrackNum = &meta.TrackNumber
		}
		if meta.Year > 0 {
			track.Year = &meta.Year
		}
		if createErr := s.db.Create(&track).Error; createErr != nil {
			return fmt.Errorf("save track: %w", createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("look up track: %w", err)
	}

	// Track exists — preserve fingerprint if already set; only recompute if missing
	if existing.Fingerprint != "" {
		fingerprint = existing.Fingerprint
	} else {
		// Track was indexed before Phase 8 introduced fingerprinting — backfill it
		fp, _, fpErr := s.metadata.Fingerprint(path)
		if fpErr == nil {
			fingerprint = fp
		}
	}

	// Update metadata fields on existing track, preserving enrichment fields
	// (genre, composer, cover_url, provenance) that the scanner does not own.
	existing.Title = meta.Title
	existing.Artist = meta.Artist
	existing.Album = meta.Album
	existing.Path = path
	existing.Format = meta.Format
	existing.FileSize = meta.FileSize
	existing.FileHash = hash
	existing.Fingerprint = fingerprint
	if meta.TrackNumber > 0 {
		existing.TrackNum = &meta.TrackNumber
	}
	if meta.Year > 0 {
		existing.Year = &meta.Year
	}
	if err := s.db.Save(&existing).Error; err != nil {
		return fmt.Errorf("update track: %w", err)
	}
	return nil
}

func (s *ScannerService) PruneTracks(ctx context.Context, libraryID uuid.UUID, jobID uint64) error {
	slog.Info("Starting prune", "library_id", libraryID, "job_id", jobID)

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
	var removed, errors int
	for _, t := range tracks {
		select {
		case <-ctx.Done():
			database.AppendJobLog(s.db, jobID, "INFO", fmt.Sprintf("Prune interrupted: %d items identified for removal, %d errors", removed, errors), nil)
			return ctx.Err()
		default:
			if _, err := os.Stat(t.Path); err != nil {
				if os.IsNotExist(err) {
					slog.Warn("Pruning missing file", "library_id", libraryID, "path", t.Path)
					database.AppendJobLog(s.db, jobID, "OK", fmt.Sprintf("Removed: %s", filepath.Base(t.Path)), nil)
					toDelete = append(toDelete, t.ID)
					removed++
				} else {
					slog.Error("Error checking file during prune", "path", t.Path, "error", err)
					database.AppendJobLog(s.db, jobID, "ERR", fmt.Sprintf("Error checking file %s: %v", filepath.Base(t.Path), err), nil)
					errors++
				}
			}
		}
	}

	if len(toDelete) > 0 {
		if err := s.db.Delete(&database.Track{}, "id IN ?", toDelete).Error; err != nil {
			database.AppendJobLog(s.db, jobID, "ERR", "Database delete operation failed", nil)
			return err
		}
	}

	kept := len(tracks) - removed
	database.AppendJobLog(s.db, jobID, "INFO", fmt.Sprintf("Prune complete: %d kept, %d removed, %d errors", kept, removed, errors), nil)
	slog.Info("Prune complete", "library_id", libraryID, "removed", removed)
	return nil
}

func (s *ScannerService) GetMonitoredArtists() ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	// ScannerService uses this for internal tasks, potentially needing all artists
	// but keeping it simple for now. If it's used by UI, it should be filtered.
	err := s.db.Preload("QualityProfile").Find(&artists).Error
	return artists, err
}

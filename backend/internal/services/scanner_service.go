package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type ScannerService struct {
	db *gorm.DB
}

func NewScannerService(db *gorm.DB) *ScannerService {
	return &ScannerService{
		db: db,
	}
}

// ScanDirectory walks the given path and imports audio files
func (s *ScannerService) ScanDirectory(path string, libraryID uuid.UUID) error {
	// Verify path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", path)
	}

	var library database.Library
	err := s.db.First(&library, "id = ?", libraryID).Error
	if err != nil {
		return fmt.Errorf("library not found: %s", libraryID)
	}

	filesProcessed := 0

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		if !isAudioFile(filePath) {
			return nil
		}

		// Process audio file
		if err := s.processFile(filePath, info, &library); err != nil {
			// Log error but continue scanning
			fmt.Printf("Error processing %s: %v\n", filePath, err)
		} else {
			filesProcessed++
		}

		return nil
	})

	return err
}

func (s *ScannerService) processFile(filePath string, info os.FileInfo, library *database.Library) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return fmt.Errorf("failed to read tags: %v", err)
	}

	// 1. Upsert Artist (Simplified: just name-based for library scan)
	artistName := m.Artist()
	if artistName == "" {
		artistName = "Unknown Artist"
	}

	// 2. Upsert Album
	albumTitle := m.Album()
	if albumTitle == "" {
		albumTitle = "Unknown Album"
	}

	if m.Year() > 0 {
		// year is available
	}

	// We'll skip complex artist/album linking during basic scan for now
	// and just populate the track record with denormalized data if needed.
	// In a full implementation, we'd match against the Artist table.

	// 3. Upsert Track
	trackTitle := m.Title()
	if trackTitle == "" {
		trackTitle = filepath.Base(filePath)
	}

	trackNum, _ := m.Track()
	discNum, _ := m.Disc()

	var track database.Track
	trackNumInt := trackNum
	discNumInt := discNum
	fileSize := info.Size()

	err = s.db.Where("path = ?", filePath).Assign(database.Track{
		LibraryID: library.ID,
		Title:     trackTitle,
		Artist:    artistName,
		Album:     albumTitle,
		TrackNum:  &trackNumInt,
		DiscNum:   &discNumInt,
		Format:    string(m.FileType()),
		FileSize:  fileSize,
		UpdatedAt: time.Now(),
	}).FirstOrCreate(&track, database.Track{
		Path:      filePath,
		LibraryID: library.ID,
		Title:     trackTitle,
	}).Error

	return err
}

func isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3", ".flac", ".m4a", ".ogg", ".wav":
		return true
	}
	return false
}

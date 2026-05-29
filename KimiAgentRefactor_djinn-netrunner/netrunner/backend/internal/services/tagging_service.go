package services

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// TaggingService handles audio file tagging and enrichment
type TaggingService struct {
	acoustidMinConfidence float64
}

// NewTaggingService creates a new tagging service
func NewTaggingService() *TaggingService {
	return &TaggingService{
		acoustidMinConfidence: 0.7, // Default confidence threshold
	}
}

// EnrichmentProvenance tracks which source wrote which tag fields
type EnrichmentProvenance struct {
	Title    string `json:"title,omitempty"`     // Source of title tag
	Artist   string `json:"artist,omitempty"`    // Source of artist tag
	Album    string `json:"album,omitempty"`     // Source of album tag
	Year     string `json:"year,omitempty"`      // Source of year tag
	Genre    string `json:"genre,omitempty"`     // Source of genre tag
	Composer string `json:"composer,omitempty"`  // Source of composer tag
	CoverArt string `json:"cover_art,omitempty"` // Source of cover art
}

// SetAcoustIDMinConfidence sets the minimum confidence threshold for AcoustID matches
func (s *TaggingService) SetAcoustIDMinConfidence(threshold float64) {
	if threshold >= 0 && threshold <= 1 {
		s.acoustidMinConfidence = threshold
	}
}

// GetAcoustIDMinConfidence returns the current minimum confidence threshold
func (s *TaggingService) GetAcoustIDMinConfidence() float64 {
	return s.acoustidMinConfidence
}

// ValidateAcoustIDConfidence checks if a confidence score meets the threshold
func (s *TaggingService) ValidateAcoustIDConfidence(confidence float64) bool {
	return confidence >= s.acoustidMinConfidence
}

// CalculateFileHash calculates SHA-256 hash of a file
func (s *TaggingService) CalculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// MarshalProvenance converts provenance to JSON string
func (s *TaggingService) MarshalProvenance(provenance *EnrichmentProvenance) (string, error) {
	if provenance == nil {
		return "", nil
	}

	data, err := json.Marshal(provenance)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// UnmarshalProvenance parses JSON string to provenance
func (s *TaggingService) UnmarshalProvenance(data string) (*EnrichmentProvenance, error) {
	if data == "" {
		return nil, nil
	}

	var provenance EnrichmentProvenance
	if err := json.Unmarshal([]byte(data), &provenance); err != nil {
		return nil, err
	}

	return &provenance, nil
}

// MergeProvenance merges two provenance records, preferring the second one for non-empty fields
func (s *TaggingService) MergeProvenance(base, override *EnrichmentProvenance) *EnrichmentProvenance {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	merged := &EnrichmentProvenance{
		Title:    base.Title,
		Artist:   base.Artist,
		Album:    base.Album,
		Year:     base.Year,
		Genre:    base.Genre,
		Composer: base.Composer,
		CoverArt: base.CoverArt,
	}

	if override.Title != "" {
		merged.Title = override.Title
	}
	if override.Artist != "" {
		merged.Artist = override.Artist
	}
	if override.Album != "" {
		merged.Album = override.Album
	}
	if override.Year != "" {
		merged.Year = override.Year
	}
	if override.Genre != "" {
		merged.Genre = override.Genre
	}
	if override.Composer != "" {
		merged.Composer = override.Composer
	}
	if override.CoverArt != "" {
		merged.CoverArt = override.CoverArt
	}

	return merged
}

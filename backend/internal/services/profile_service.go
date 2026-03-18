package services

import (
	"fmt"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// ProfileService handles quality profile operations
type ProfileService struct {
	db *gorm.DB
}

// NewProfileService creates a new ProfileService
func NewProfileService(db *gorm.DB) *ProfileService {
	return &ProfileService{db: db}
}

// GetDefaultProfile returns the default quality profile
func (s *ProfileService) GetDefaultProfile() (*database.QualityProfile, error) {
	var profile database.QualityProfile
	err := s.db.Where("is_default = ?", true).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// GetProfileByID returns a profile by ID
func (s *ProfileService) GetProfileByID(id string) (*database.QualityProfile, error) {
	var profile database.QualityProfile
	err := s.db.First(&profile, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// ListProfiles returns all profiles
func (s *ProfileService) ListProfiles() ([]database.QualityProfile, error) {
	var profiles []database.QualityProfile
	err := s.db.Order("name").Find(&profiles).Error
	return profiles, err
}

// EnsureDefaultProfile ensures a default profile exists, creating one if needed
func (s *ProfileService) EnsureDefaultProfile() (*database.QualityProfile, error) {
	var profile database.QualityProfile
	err := s.db.Where("is_default = ?", true).First(&profile).Error
	if err == nil {
		return &profile, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// Create default profile
	profile = database.QualityProfile{
		Name:                "Default",
		Description:         "Standard quality profile for music acquisition",
		PreferLossless:      true,
		AllowedFormats:      "FLAC,ALAC,WAV",
		MinBitrate:          0,
		PreferSceneReleases: false,
		PreferWebReleases:   true,
		IsDefault:           true,
	}

	if err := s.db.Create(&profile).Error; err != nil {
		return nil, err
	}

	return &profile, nil
}

// FormatValidationResult holds validation details for display
type FormatValidationResult struct {
	Valid      bool     `json:"valid"`
	Format     string   `json:"format"`
	Bitrate    int      `json:"bitrate"`
	Allowed    []string `json:"allowed_formats,omitempty"`
	MinBitrate int      `json:"min_bitrate,omitempty"`
	Issues     []string `json:"issues"`
}

// ValidateFull checks all criteria and returns detailed results
func (s *ProfileService) ValidateFull(profile *database.QualityProfile, format string, bitrate int) FormatValidationResult {
	result := FormatValidationResult{
		Format:  format,
		Bitrate: bitrate,
		Valid:   true,
		Issues:  []string{},
	}

	if profile.AllowedFormats != "" {
		result.Allowed = strings.Split(profile.AllowedFormats, ",")
	}

	if profile.MinBitrate > 0 {
		result.MinBitrate = profile.MinBitrate
	}

	// Check format and bitrate using the model's IsMatch method
	if !profile.IsMatch(format, bitrate) {
		result.Valid = false

		// Determine specific issue
		if profile.AllowedFormats != "" {
			allowed := strings.Split(strings.ToLower(profile.AllowedFormats), ",")
			currentFormat := strings.ToLower(format)
			currentFormat = strings.TrimPrefix(currentFormat, ".")
			matched := false
			for _, f := range allowed {
				if strings.TrimSpace(f) == currentFormat {
					matched = true
					break
				}
			}
			if !matched {
				result.Issues = append(result.Issues, fmt.Sprintf("format %s not in allowed list", format))
			} else if bitrate > 0 && bitrate < profile.MinBitrate {
				result.Issues = append(result.Issues, fmt.Sprintf("bitrate %d below minimum %d", bitrate, profile.MinBitrate))
			}
		} else if bitrate > 0 && bitrate < profile.MinBitrate {
			result.Issues = append(result.Issues, fmt.Sprintf("bitrate %d below minimum %d", bitrate, profile.MinBitrate))
		}
	}

	return result
}

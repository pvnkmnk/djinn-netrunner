package database

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// CalculateBackoff returns the duration to wait before retrying based on attempt count
func CalculateBackoff(retryCount int) time.Duration {
	switch retryCount {
	case 0:
		return 1 * time.Minute
	case 1:
		return 5 * time.Minute
	case 2:
		return 1 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// AppendJobLog appends a log entry to a job
func AppendJobLog(db *gorm.DB, jobID uint64, level, message string, itemID *uint64) error {
	log := JobLog{
		JobID:     jobID,
		JobItemID: itemID,
		Level:     level,
		Message:   message,
	}
	return db.Create(&log).Error
}

// IsMatch checks if a file (by format and bitrate) matches the quality profile
func (p *QualityProfile) IsMatch(format string, bitrate int) bool {
	// 1. Check format
	if p.AllowedFormats != "" {
		allowed := strings.Split(strings.ToLower(p.AllowedFormats), ",")
		matchedFormat := false
		currentFormat := strings.ToLower(format)
		// Remove leading dot if present
		currentFormat = strings.TrimPrefix(currentFormat, ".")

		for _, f := range allowed {
			if strings.TrimSpace(f) == currentFormat {
				matchedFormat = true
				break
			}
		}
		if !matchedFormat {
			return false
		}
	}

	// 2. Check lossless preference
	isLossless := strings.EqualFold(format, "flac") || strings.EqualFold(format, ".flac") ||
		strings.EqualFold(format, "wav") || strings.EqualFold(format, ".wav") ||
		strings.EqualFold(format, "alac") || strings.EqualFold(format, "aiff")

	if p.PreferLossless && !isLossless {
		// If we prefer lossless, we only accept lossy if it meets minimum bitrate
		if bitrate < p.MinBitrate {
			return false
		}
	} else if !isLossless && bitrate < p.MinBitrate {
		// Even if we don't prefer lossless, we respect the minimum bitrate for lossy files
		return false
	}

	return true
}

// GetSearchSuffix returns a string suffix to append to searches based on the profile
func (p *QualityProfile) GetSearchSuffix() string {
	if p.PreferLossless {
		return "flac"
	}
	if p.PreferBitrate != nil && *p.PreferBitrate >= 320 {
		return "320"
	}
	return ""
}

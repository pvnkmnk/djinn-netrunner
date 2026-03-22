package services

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// DiskQuotaService calculates library disk usage and checks quota thresholds.
type DiskQuotaService struct {
	db *sql.DB
}

// NewDiskQuotaService creates a new DiskQuotaService.
func NewDiskQuotaService(db *sql.DB) *DiskQuotaService {
	return &DiskQuotaService{db: db}
}

// LibraryUsage holds usage statistics for a library.
type LibraryUsage struct {
	LibraryID   string
	LibraryName string
	UsedBytes   int64
	LimitBytes  int64
	UsedPct     int // percentage: 0-100+
}

// CalculateLibraryUsage returns the total byte size of all tracks in a library.
func (s *DiskQuotaService) CalculateLibraryUsage(libraryID string) (int64, error) {
	var total int64
	err := s.db.QueryRow(
		"SELECT COALESCE(SUM(file_size), 0) FROM tracks WHERE library_id = ?", libraryID,
	).Scan(&total)
	return total, err
}

// GetLibraryUsage returns usage stats for a library, including limit and percentage.
func (s *DiskQuotaService) GetLibraryUsage(lib database.Library) (*LibraryUsage, error) {
	used, err := s.CalculateLibraryUsage(lib.ID.String())
	if err != nil {
		return nil, err
	}

	usage := &LibraryUsage{
		LibraryID:   lib.ID.String(),
		LibraryName: lib.Name,
		UsedBytes:   used,
		LimitBytes:  0,
		UsedPct:     0,
	}

	// Use per-library quota if set
	if lib.MaxSizeBytes != nil {
		usage.LimitBytes = *lib.MaxSizeBytes
	} else {
		// Fall back to filesystem-level total capacity
		var free int64
		usage.LimitBytes, free, err = getFilesystemUsage(lib.Path)
		if err != nil {
			// Log but don't fail — filesystem stats are best-effort
			usage.LimitBytes = 0
		} else {
			usage.UsedBytes = usage.LimitBytes - free
		}
	}

	if usage.LimitBytes > 0 {
		usage.UsedPct = int(float64(usage.UsedBytes) / float64(usage.LimitBytes) * 100)
	}

	return usage, nil
}

// CheckQuotaAlert checks if a library has exceeded its alert threshold.
// Returns the LibraryUsage if over threshold, or nil otherwise.
func (s *DiskQuotaService) CheckQuotaAlert(lib database.Library) (*LibraryUsage, error) {
	threshold := 80
	if lib.QuotaAlertAt != nil {
		threshold = *lib.QuotaAlertAt
	}

	usage, err := s.GetLibraryUsage(lib)
	if err != nil {
		return nil, err
	}

	if usage.LimitBytes > 0 && usage.UsedPct >= threshold {
		return usage, nil
	}
	return nil, nil
}

// CheckAllLibraryQuotas checks all libraries and returns those over threshold.
func (s *DiskQuotaService) CheckAllLibraryQuotas() ([]LibraryUsage, error) {
	rows, err := s.db.Query("SELECT id, name, path, max_size_bytes, quota_alert_at FROM libraries")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []LibraryUsage
	for rows.Next() {
		var lib struct {
			ID           uuid.UUID
			Name         string
			Path         string
			MaxSizeBytes sql.NullInt64
			QuotaAlertAt sql.NullInt64
		}
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Path, &lib.MaxSizeBytes, &lib.QuotaAlertAt); err != nil {
			continue
		}
		dbLib := database.Library{ID: lib.ID, Name: lib.Name, Path: lib.Path}
		if lib.MaxSizeBytes.Valid {
			size := lib.MaxSizeBytes.Int64
			dbLib.MaxSizeBytes = &size
		}
		if lib.QuotaAlertAt.Valid {
			at := int(lib.QuotaAlertAt.Int64)
			dbLib.QuotaAlertAt = &at
		}

		alert, err := s.CheckQuotaAlert(dbLib)
		if err != nil {
			continue
		}
		if alert != nil {
			alerts = append(alerts, *alert)
		}
	}
	return alerts, nil
}

// FormatBytes converts bytes to a human-readable string.
func FormatBytes(bytes int64) string {
	const unit = int64(1024)
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := unit, 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

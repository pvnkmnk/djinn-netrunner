package agent

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

// SystemStatus represents the health of the NetRunner appliance
type SystemStatus struct {
	DatabaseConnected bool   `json:"database_connected"`
	SlskdConnected    bool   `json:"slskd_connected"`
	GonicConnected    bool   `json:"gonic_connected"`
	Message           string `json:"message"`
}

// ProbeSystem checks the connectivity of various system components
func ProbeSystem(db *gorm.DB, cfg *config.Config) (*SystemStatus, error) {
	status := &SystemStatus{}

	// 1. Check Database
	sqlDB, err := db.DB()
	if err == nil {
		err = sqlDB.Ping()
		if err == nil {
			status.DatabaseConnected = true
		}
	}

	// 2. Check Gonic (if configured)
	if cfg.GonicURL != "" {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(cfg.GonicURL + "/ping")
		if err == nil && resp.StatusCode == http.StatusOK {
			status.GonicConnected = true
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// 3. Check Slskd
	if cfg.SlskdURL != "" {
		slskdClient := services.NewSlskdService(cfg, db)
		if slskdClient.HealthCheck() {
			status.SlskdConnected = true
		}
	}

	if status.DatabaseConnected {
		status.Message = "System partially operational."
		if status.GonicConnected {
			status.Message = "System fully operational."
		}
	} else {
		status.Message = "Critical failure: Database disconnected."
	}

	return status, nil
}

// ReadConfig returns the current system configuration (non-sensitive)
func ReadConfig(db *gorm.DB, cfg *config.Config) (map[string]string, error) {
	settings := make(map[string]string)

	// Add static config
	settings["port"] = cfg.Port
	settings["environment"] = cfg.Environment
	settings["gonic_url"] = cfg.GonicURL
	settings["proxy_url"] = cfg.ProxyURL

	// Add dynamic settings from DB
	var dbSettings []database.Setting
	if err := db.Find(&dbSettings).Error; err == nil {
		for _, s := range dbSettings {
			settings[s.Key] = s.Value
		}
	}

	return settings, nil
}

// UpdateConfig updates a dynamic setting in the database
func UpdateConfig(db *gorm.DB, key, value string) error {
	setting := database.Setting{
		Key:   key,
		Value: value,
	}
	return db.Save(&setting).Error
}

// ListWatchlists returns all registered watchlists
func ListWatchlists(s *services.WatchlistService) ([]database.Watchlist, error) {
	return s.GetWatchlists()
}

// AddWatchlist adds a new watchlist using the service
func AddWatchlist(s *services.WatchlistService, name, sourceType, uri string, profileID uuid.UUID, userID *uint64) (*database.Watchlist, error) {
	return s.CreateWatchlist(name, sourceType, uri, profileID, userID)
}

// SyncWatchlist triggers a sync job for a specific watchlist
func SyncWatchlist(db *gorm.DB, watchlistID uuid.UUID, userID *uint64) (*database.Job, error) {
	job := database.Job{
		Type:        "sync",
		State:       "queued",
		ScopeType:   "watchlist",
		ScopeID:     watchlistID.String(),
		RequestedAt: time.Now(),
		OwnerUserID: userID,
		CreatedBy:   "cli",
	}

	if err := db.Create(&job).Error; err != nil {
		return nil, err
	}

	return &job, nil
}

// ListLibraries returns all registered libraries
func ListLibraries(db *gorm.DB) ([]database.Library, error) {
	var libraries []database.Library
	err := db.Order("name").Find(&libraries).Error
	return libraries, err
}

// AddLibrary adds a new library
func AddLibrary(db *gorm.DB, name, path string) (*database.Library, error) {
	library := database.Library{
		ID:   uuid.New(),
		Name: name,
		Path: path,
	}

	if err := db.Create(&library).Error; err != nil {
		return nil, err
	}

	return &library, nil
}

// DeleteLibrary deletes a library by ID
func DeleteLibrary(db *gorm.DB, libraryID uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// First delete associated tracks
		if err := tx.Delete(&database.Track{}, "library_id = ?", libraryID).Error; err != nil {
			return err
		}
		// Then delete the library
		return tx.Delete(&database.Library{}, "id = ?", libraryID).Error
	})
}

// ScanLibrary triggers a scan job for a specific library
func ScanLibrary(db *gorm.DB, libraryID uuid.UUID) (*database.Job, error) {
	// Verify library exists
	var library database.Library
	if err := db.First(&library, "id = ?", libraryID).Error; err != nil {
		return nil, err
	}

	job := database.Job{
		Type:        "scan",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     libraryID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "cli",
	}

	if err := db.Create(&job).Error; err != nil {
		return nil, err
	}

	return &job, nil
}

// ListMonitoredArtists returns all monitored artists with their release counts.
func ListMonitoredArtists(db *gorm.DB) ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	err := db.Find(&artists).Error
	return artists, err
}

// CancelJob cancels a queued or running job.
func CancelJob(db *gorm.DB, jobID uint64) error {
	result := db.Model(&database.Job{}).
		Where("id = ? AND state IN ?", jobID, []string{"queued", "running"}).
		Update("state", "cancelled")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("job %d not found or cannot be cancelled (must be queued or running)", jobID)
	}
	// Also cancel any pending job items
	db.Model(&database.JobItem{}).
		Where("job_id = ? AND state IN ?", jobID, []string{"queued", "running"}).
		Update("state", "cancelled")
	return nil
}

// RetryJob retries a failed job by resetting its failed items to queued.
func RetryJob(db *gorm.DB, jobID uint64) error {
	var job database.Job
	if err := db.First(&job, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("job %d not found", jobID)
	}
	if job.State != "failed" {
		return fmt.Errorf("job %d is not in failed state (current: %s)", jobID, job.State)
	}
	// Reset failed items to queued
	db.Model(&database.JobItem{}).
		Where("job_id = ? AND state IN ?", jobID, []string{"failed", "completed (duplicate hash)", "completed (already indexed)"}).
		Update("state", "queued")
	// Reset job to queued
	return db.Model(&job).Update("state", "queued").Error
}

// ListJobs returns recent and active background jobs
func ListJobs(db *gorm.DB, limit int) ([]database.Job, error) {
	var jobs []database.Job
	err := db.Order("requested_at DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// GetJobLogs returns structured logs for a specific job
func GetJobLogs(db *gorm.DB, jobID uint64) ([]database.JobLog, error) {
	var logs []database.JobLog
	err := db.Where("job_id = ?", jobID).Order("created_at ASC").Find(&logs).Error
	return logs, err
}

// EnqueueAcquisition manually triggers a new acquisition job
func EnqueueAcquisition(db *gorm.DB, artist, album, title string, userID *uint64) (*database.Job, error) {
	job := database.Job{
		Type:        "acquisition",
		State:       "queued",
		RequestedAt: time.Now(),
		OwnerUserID: userID,
		CreatedBy:   "agent_interface",
	}

	if err := db.Create(&job).Error; err != nil {
		return nil, err
	}

	item := database.JobItem{
		JobID:           job.ID,
		Artist:          artist,
		Album:           album,
		TrackTitle:      title,
		NormalizedQuery: fmt.Sprintf("%s %s", artist, title),
		Status:          "queued",
		OwnerUserID:     userID,
	}

	if err := db.Create(&item).Error; err != nil {
		return nil, err
	}

	return &job, nil
}

// Bootstrap checks the environment and performs initial system setup
func Bootstrap(db *gorm.DB, cfg *config.Config) (map[string]string, error) {
	results := make(map[string]string)

	// 1. Check required Env vars
	requiredEnv := map[string]string{
		"DATABASE_URL": cfg.DatabaseURL,
		"GONIC_URL":    cfg.GonicURL,
	}

	for key, val := range requiredEnv {
		if val == "" {
			results[key] = "MISSING"
		} else {
			results[key] = "OK"
		}
	}

	// 2. Connectivity check
	status, _ := ProbeSystem(db, cfg)
	if status.DatabaseConnected {
		results["DATABASE_CONN"] = "OK"
	} else {
		results["DATABASE_CONN"] = "FAILED"
	}

	if status.GonicConnected {
		results["GONIC_CONN"] = "OK"
	} else {
		results["GONIC_CONN"] = "FAILED"
	}

	// 3. Ensure tables exist (Migration check)
	err := db.AutoMigrate(
		&database.User{},
		&database.QualityProfile{},
		&database.Watchlist{},
		&database.Job{},
		&database.JobItem{},
		&database.JobLog{},
		&database.Setting{},
		&database.Acquisition{},
	)
	if err != nil {
		results["MIGRATIONS"] = fmt.Sprintf("FAILED: %v", err)
	} else {
		results["MIGRATIONS"] = "OK"
	}

	return results, nil
}

// RegisterWebhook registers a callback URL for agent notifications
func RegisterWebhook(db *gorm.DB, url string) error {
	return UpdateConfig(db, "agent_notification_webhook", url)
}

// SearchLibrary queries the local DB and Gonic for tracks matching the query
func SearchLibrary(db *gorm.DB, gonic *services.GonicClient, query string) ([]map[string]string, error) {
	var results []map[string]string

	// 1. Search local acquisitions
	var acquisitions []database.Acquisition
	searchQuery := "%" + query + "%"
	err := db.Where("artist LIKE ? OR track_title LIKE ? OR album LIKE ?", searchQuery, searchQuery, searchQuery).
		Limit(50).Find(&acquisitions).Error

	if err == nil {
		for _, a := range acquisitions {
			results = append(results, map[string]string{
				"artist": a.Artist,
				"title":  a.TrackTitle,
				"album":  a.Album,
				"source": "local",
				"path":   a.FinalPath,
			})
		}
	}

	// 2. Search Gonic
	if gonic != nil {
		gonicTracks, err := gonic.Search3(query)
		if err == nil {
			for _, t := range gonicTracks {
				results = append(results, map[string]string{
					"artist": t.Artist,
					"title":  t.Title,
					"album":  t.Album,
					"source": "gonic",
					"id":     t.ID,
				})
			}
		}
	}

	return results, nil
}

// Stats types for CLI
type JobStats struct {
	Total       int64   `json:"total"`
	Queued      int64   `json:"queued"`
	Running     int64   `json:"running"`
	Succeeded   int64   `json:"succeeded"`
	Failed      int64   `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
}

type LibraryStats struct {
	TotalTracks     int64         `json:"total_tracks"`
	TotalSize       int64         `json:"total_size"`
	TotalSizeMB     float64       `json:"total_size_mb"`
	FormatBreakdown []FormatCount `json:"format_breakdown"`
}

type FormatCount struct {
	Format    string `json:"format"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

type ActivityStats struct {
	MonitoredArtists int64 `json:"monitored_artists"`
	Watchlists       int64 `json:"watchlists"`
	Libraries        int64 `json:"libraries"`
}

type SummaryStats struct {
	Jobs     JobStats      `json:"jobs"`
	Library  LibraryStats  `json:"library"`
	Activity ActivityStats `json:"activity"`
}

// GetJobStats returns job statistics
func GetJobStats(db *gorm.DB) (*JobStats, error) {
	since := time.Now().Add(-24 * time.Hour)

	var stats JobStats
	err := db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) as total, " +
			"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	completed := stats.Succeeded + stats.Failed
	if completed > 0 {
		stats.SuccessRate = float64(stats.Succeeded) / float64(completed) * 100
	}

	return &stats, nil
}

// GetLibraryStats returns library statistics
func GetLibraryStats(db *gorm.DB) (*LibraryStats, error) {
	var stats LibraryStats

	err := db.Model(&database.Track{}).
		Select("COUNT(*) as total_tracks, COALESCE(SUM(file_size), 0) as total_size").
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	stats.TotalSizeMB = float64(stats.TotalSize) / (1024 * 1024)

	// Format breakdown
	err = db.Model(&database.Track{}).
		Select("format, COUNT(*) as count, COALESCE(SUM(file_size), 0) as total_size").
		Group("format").
		Order("count DESC").
		Scan(&stats.FormatBreakdown).Error

	return &stats, err
}

// GetStatsSummary returns combined summary statistics
func GetStatsSummary(db *gorm.DB) (*SummaryStats, error) {
	var summary SummaryStats

	// Job stats (24h)
	since := time.Now().Add(-24 * time.Hour)
	err := db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) as total, " +
			"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&summary.Jobs).Error
	if err != nil {
		return nil, err
	}

	completed := summary.Jobs.Succeeded + summary.Jobs.Failed
	if completed > 0 {
		summary.Jobs.SuccessRate = float64(summary.Jobs.Succeeded) / float64(completed) * 100
	}

	// Library stats
	err = db.Model(&database.Track{}).
		Select("COUNT(*) as total_tracks, COALESCE(SUM(file_size), 0) as total_size").
		Scan(&summary.Library).Error
	if err != nil {
		return nil, err
	}
	summary.Library.TotalSizeMB = float64(summary.Library.TotalSize) / (1024 * 1024)

	// Activity stats
	if err := db.Model(&database.MonitoredArtist{}).Count(&summary.Activity.MonitoredArtists).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&database.Watchlist{}).Count(&summary.Activity.Watchlists).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&database.Library{}).Count(&summary.Activity.Libraries).Error; err != nil {
		return nil, err
	}

	return &summary, nil
}

// ListProfiles returns all quality profiles
func ListProfiles(db *gorm.DB) ([]database.QualityProfile, error) {
	var profiles []database.QualityProfile
	err := db.Order("name").Find(&profiles).Error
	return profiles, err
}

// GetProfile returns a single profile by ID
func GetProfile(db *gorm.DB, profileID uuid.UUID) (*database.QualityProfile, error) {
	var profile database.QualityProfile
	err := db.First(&profile, "id = ?", profileID).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// CreateProfile creates a new quality profile
func CreateProfile(db *gorm.DB, name, description string, preferLossless bool, allowedFormats string, minBitrate int, preferBitrate *int, preferScene, preferWeb bool) (*database.QualityProfile, error) {
	// If setting as default, clear other defaults
	if err := db.Model(&database.QualityProfile{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
		return nil, err
	}

	profile := database.QualityProfile{
		ID:                  uuid.New(),
		Name:                name,
		Description:         description,
		PreferLossless:      preferLossless,
		AllowedFormats:      allowedFormats,
		MinBitrate:          minBitrate,
		PreferBitrate:       preferBitrate,
		PreferSceneReleases: preferScene,
		PreferWebReleases:   preferWeb,
		IsDefault:           true,
	}

	if err := db.Create(&profile).Error; err != nil {
		return nil, err
	}

	return &profile, nil
}

// DeleteProfile deletes a profile by ID
func DeleteProfile(db *gorm.DB, profileID uuid.UUID) error {
	// Check if profile is in use
	var count int64
	if err := db.Model(&database.Watchlist{}).Where("quality_profile_id = ?", profileID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("profile is in use by watchlists")
	}

	if err := db.Model(&database.MonitoredArtist{}).Where("quality_profile_id = ?", profileID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("profile is in use by monitored artists")
	}

	return db.Delete(&database.QualityProfile{}, "id = ?", profileID).Error
}

// SetDefaultProfile sets a profile as the default
func SetDefaultProfile(db *gorm.DB, profileID uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// Clear existing defaults
		if err := tx.Model(&database.QualityProfile{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
			return err
		}

		// Set new default
		return tx.Model(&database.QualityProfile{}).Where("id = ?", profileID).Update("is_default", true).Error
	})
}

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

	// 3. Slskd check (stub for now, will implement client later)
	status.SlskdConnected = false // TODO: Implement slskd ping

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
	// First delete associated tracks
	if err := db.Delete(&database.Track{}, "library_id = ?", libraryID).Error; err != nil {
		return err
	}
	// Then delete the library
	return db.Delete(&database.Library{}, "id = ?", libraryID).Error
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

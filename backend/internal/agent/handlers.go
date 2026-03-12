package agent

import (
	"net/http"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SystemStatus represents the health of the NetRunner appliance
type SystemStatus struct {
	DatabaseConnected bool   `json:"database_connected"`
	SlskdConnected    bool   `json:"slskd_connected"`
	GonicConnected     bool   `json:"gonic_connected"`
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

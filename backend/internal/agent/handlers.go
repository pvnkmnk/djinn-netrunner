package agent

import (
	"net/http"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
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

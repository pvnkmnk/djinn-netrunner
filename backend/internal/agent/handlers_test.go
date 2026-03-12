package agent

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestProbeSystem(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	cfg := &config.Config{
		GonicURL: "http://localhost:14747",
	}

	status, err := ProbeSystem(db, cfg)
	assert.NoError(t, err)
	assert.True(t, status.DatabaseConnected)
	// We expect Gonic to fail in this test environment
	assert.False(t, status.GonicConnected)
}

func TestConfigTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Setting{})

	cfg := &config.Config{
		Port: "8080",
	}


	// Test ReadConfig
	settings, err := ReadConfig(db, cfg)
	assert.NoError(t, err)
	assert.Equal(t, "8080", settings["port"])

	// Test UpdateConfig
	err = UpdateConfig(db, "custom_setting", "custom_value")
	assert.NoError(t, err)

	// Verify update
	var setting database.Setting
	err = db.First(&setting, "key = ?", "custom_setting").Error
	assert.NoError(t, err)
	assert.Equal(t, "custom_value", setting.Value)
}

func TestWatchlistTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Watchlist{}, &database.QualityProfile{}, &database.User{})

	// Create a default profile and user
	profile := database.QualityProfile{Name: "Standard"}
	db.Create(&profile)
	user := database.User{Email: "agent@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	service := services.NewWatchlistService(db, nil, &config.Config{})
	
	// Test AddWatchlist
	wl, err := AddWatchlist(service, "Test Watchlist", "lastfm_loved", "testuser", profile.ID, &user.ID)
	assert.NoError(t, err)
	assert.Equal(t, "Test Watchlist", wl.Name)

	// Test ListWatchlists
	lists, err := ListWatchlists(service)
	assert.NoError(t, err)
	assert.Len(t, lists, 1)
}

func TestJobMonitoringTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.JobLog{})

	// Create a test job and log
	job := database.Job{Type: "acquisition", State: "running"}
	db.Create(&job)
	db.Create(&database.JobLog{JobID: job.ID, Level: "info", Message: "Starting test job"})

	// Test ListJobs
	jobs, err := ListJobs(db, 10)
	assert.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "running", jobs[0].State)

	// Test GetJobLogs
	logs, err := GetJobLogs(db, job.ID)
	assert.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "Starting test job", logs[0].Message)
}

package agent

import (
	"net/http"
	"net/http/httptest"
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

func TestProbeSystemSlskdConnected(t *testing.T) {
	// Setup a mock slskd server that responds OK to /api/v0/session
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/session" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	cfg := &config.Config{
		SlskdURL: server.URL, // no trailing slash — HealthCheck appends /api/v0/session
	}

	slskdService := services.NewSlskdService(cfg)
	assert.True(t, slskdService.HealthCheck(), "mock slskd server should be reachable")

	status, err := ProbeSystem(db, cfg)
	assert.NoError(t, err)
	assert.True(t, status.DatabaseConnected)
	assert.True(t, status.SlskdConnected, "SlskdConnected should be true when slskd server responds OK")
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

func TestLibraryTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Library{}, &database.Track{}, &database.Job{}, &database.User{})

	user := database.User{Email: "library@test.com", PasswordHash: "xxx"}
	db.Create(&user)

	// Test AddLibrary
	library, err := AddLibrary(db, "Test Library", "/music/test")
	assert.NoError(t, err)
	assert.Equal(t, "Test Library", library.Name)
	assert.Equal(t, "/music/test", library.Path)

	// Test ListLibraries
	libraries, err := ListLibraries(db)
	assert.NoError(t, err)
	assert.Len(t, libraries, 1)

	// Test ScanLibrary - should create a job
	job, err := ScanLibrary(db, library.ID)
	assert.NoError(t, err)
	assert.Equal(t, "scan", job.Type)
	assert.Equal(t, "queued", job.State)

	// Test DeleteLibrary
	err = DeleteLibrary(db, library.ID)
	assert.NoError(t, err)

	// Verify library is deleted
	libraries, err = ListLibraries(db)
	assert.NoError(t, err)
	assert.Len(t, libraries, 0)
}

func TestJobMonitoringTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Job{}, &database.JobLog{}, &database.User{}, &database.JobItem{})

	user := database.User{Email: "job@test.com", PasswordHash: "xxx"}
	db.Create(&user)

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

	// Test EnqueueAcquisition
	newJob, err := EnqueueAcquisition(db, "New Artist", "New Album", "New Track", &user.ID)
	assert.NoError(t, err)
	assert.Equal(t, "acquisition", newJob.Type)

	var newItem database.JobItem
	err = db.Where("job_id = ?", newJob.ID).First(&newItem).Error
	assert.NoError(t, err)
	assert.Equal(t, "New Artist", newItem.Artist)
}

func TestBootstrap(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Mock config
	cfg := &config.Config{
		GonicURL: "http://localhost:14747",
	}

	// For now, bootstrap just checks env and returns status
	results, err := Bootstrap(db, cfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestSearchLibrary(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Acquisition{})

	// Create a test acquisition
	db.Create(&database.Acquisition{
		Artist:     "Local Artist",
		TrackTitle: "Local Track",
		FinalPath:  "/path/to/music.mp3",
	})

	// Test SearchLibrary (Local only for now)
	results, err := SearchLibrary(db, nil, "Local")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Local Artist", results[0]["artist"])
}

func TestAgentNotification(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Setting{})

	// Test RegisterWebhook
	err = RegisterWebhook(db, "http://agent-callback.com/webhook")
	assert.NoError(t, err)

	// Verify setting
	var setting database.Setting
	err = db.First(&setting, "key = ?", "agent_notification_webhook").Error
	assert.NoError(t, err)
	assert.Equal(t, "http://agent-callback.com/webhook", setting.Value)
}

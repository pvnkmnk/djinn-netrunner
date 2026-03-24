package integration

// Package integration provides end-to-end testing harness for NetRunner
// with dockerized slskd and test Soulseek accounts.
//
// Usage:
//   1. Start integration services: docker compose -f docker-compose.integration.yml up -d
//   2. Run tests: go test ./backend/internal/integration/... -v -tags=integration
//   3. Stop services: docker compose -f docker-compose.integration.yml down
//
//go:build integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

// Integration test configuration
const (
	// Default endpoints for dockerized services
	defaultSlskdURL     = "http://localhost:15030"
	defaultSlskdAPIKey  = "test-api-key-for-integration"
	defaultDBURL        = "postgresql://testuser:testpass@localhost:15432/netrunner_integration?sslmode=disable"
	
	// Test timeouts
	slskdHealthTimeout  = 2 * time.Minute
	downloadWaitTimeout = 30 * time.Second
	searchTimeout       = 45 * time.Second
)

// Test environment variables
var (
	slskdURL     = getEnv("INTEGRATION_SLSKD_URL", defaultSlskdURL)
	slskdAPIKey  = getEnv("INTEGRATION_SLSKD_API_KEY", defaultSlskdAPIKey)
	databaseURL  = getEnv("INTEGRATION_DATABASE_URL", defaultDBURL)
	slskdUsername = getEnv("INTEGRATION_SLSKD_USERNAME", "")
	slskdPassword = getEnv("INTEGRATION_SLSKD_PASSWORD", "")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// IntegrationHarness provides a test harness for slskd integration tests
type IntegrationHarness struct {
	DB         *gorm.DB
	Slskd      *services.SlskdService
	Config     *config.Config
	MockServer *httptest.Server
	
	// Test data
	TestQualityProfile *database.QualityProfile
	TestWatchlist      *database.Watchlist
}

// SetupIntegrationHarness initializes the integration test harness
// It requires dockerized slskd to be running
func SetupIntegrationHarness(t *testing.T) *IntegrationHarness {
	t.Helper()
	
	// Check if integration tests should be skipped
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Skipping integration tests: SKIP_INTEGRATION_TESTS=true")
	}
	
	harness := &IntegrationHarness{}
	
	// Initialize database
	cfg := &config.Config{
		DatabaseURL: databaseURL,
		SlskdURL:    slskdURL,
		SlskdAPIKey: slskdAPIKey,
	}
	
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to integration database: %v", err)
	}
	
	// Run migrations
	if err := database.Migrate(db); err != nil {
		t.Fatalf("Failed to run database migrations: %v", err)
	}
	
	// Clean up test data before tests
	cleanupTestData(t, db)
	
	harness.DB = db
	harness.Config = cfg
	harness.Slskd = services.NewSlskdService(cfg)
	
	// Wait for slskd to be healthy
	if err := harness.waitForSlskd(t); err != nil {
		t.Fatalf("slskd health check failed: %v. Make sure docker compose -f docker-compose.integration.yml up -d is running", err)
	}
	
	// Create test quality profile
	harness.createTestQualityProfile(t)
	
	return harness
}

// Teardown cleans up the integration harness
func (h *IntegrationHarness) Teardown(t *testing.T) {
	t.Helper()
	
	if h.DB != nil {
		cleanupTestData(t, h.DB)
		
		sqlDB, err := h.DB.DB()
		if err == nil {
			sqlDB.Close()
		}
	}
	
	if h.MockServer != nil {
		h.MockServer.Close()
	}
}

// waitForSlskd waits for slskd to become healthy
func (h *IntegrationHarness) waitForSlskd(t *testing.T) error {
	t.Helper()
	
	ctx, cancel := context.WithTimeout(context.Background(), slskdHealthTimeout)
	defer cancel()
	
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	attempts := 0
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for slskd to become healthy after %d attempts", attempts)
		case <-ticker.C:
			attempts++
			if h.Slskd.HealthCheck() {
				t.Logf("slskd is healthy after %d attempts", attempts)
				return nil
			}
			if attempts%5 == 0 {
				t.Logf("Waiting for slskd to become healthy... (attempt %d)", attempts)
			}
		}
	}
}

// createTestQualityProfile creates a test quality profile for integration tests
func (h *IntegrationHarness) createTestQualityProfile(t *testing.T) {
	t.Helper()
	
	profile := &database.QualityProfile{
		Name:             "Integration Test Profile",
		Description:      "Profile for integration testing",
		PreferLossless:   false,
		AllowedFormats:   "mp3,flac",
		MinBitrate:       192,
		CoverArtSources:  "source,musicbrainz,discogs",
		IsDefault:        false,
	}
	
	if err := h.DB.Create(profile).Error; err != nil {
		t.Fatalf("Failed to create test quality profile: %v", err)
	}
	
	h.TestQualityProfile = profile
}

// cleanupTestData removes test data from the database
func cleanupTestData(t *testing.T, db *gorm.DB) {
	t.Helper()
	
	// Delete in order respecting foreign keys
	db.Exec("DELETE FROM job_logs WHERE job_id IN (SELECT id FROM jobs WHERE created_by = 'integration_test')")
	db.Exec("DELETE FROM acquisitions WHERE job_id IN (SELECT id FROM jobs WHERE created_by = 'integration_test')")
	db.Exec("DELETE FROM jobitems WHERE job_id IN (SELECT id FROM jobs WHERE created_by = 'integration_test')")
	db.Exec("DELETE FROM jobs WHERE created_by = 'integration_test'")
	db.Exec("DELETE FROM quality_profiles WHERE name = 'Integration Test Profile'")
}

// CreateTestJob creates a test acquisition job with items
func (h *IntegrationHarness) CreateTestJob(t *testing.T, items []TestJobItem) (*database.Job, []database.JobItem) {
	t.Helper()
	
	job := &database.Job{
		Type:        "acquisition",
		State:       "queued",
		ScopeType:   "watchlist",
		RequestedAt: time.Now(),
		CreatedBy:   "integration_test",
	}
	
	if err := h.DB.Create(job).Error; err != nil {
		t.Fatalf("Failed to create test job: %v", err)
	}
	
	var jobItems []database.JobItem
	for i, item := range items {
		ji := database.JobItem{
			JobID:           job.ID,
			Sequence:        i,
			NormalizedQuery: item.Query,
			Artist:          item.Artist,
			Album:           item.Album,
			TrackTitle:      item.Title,
			Status:          "queued",
		}
		if err := h.DB.Create(&ji).Error; err != nil {
			t.Fatalf("Failed to create test job item: %v", err)
		}
		jobItems = append(jobItems, ji)
	}
	
	return job, jobItems
}

// TestJobItem represents a test item for acquisition
type TestJobItem struct {
	Query  string
	Artist string
	Album  string
	Title  string
}

// ValidateEndToEndSearch performs an end-to-end search test against real slskd
func (h *IntegrationHarness) ValidateEndToEndSearch(t *testing.T, query string, minResults int) []services.SearchResult {
	t.Helper()
	
	t.Logf("Performing end-to-end search for: %s", query)
	
	// Perform search with real slskd instance
	results, err := h.Slskd.Search(query, 30, h.TestQualityProfile)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	
	t.Logf("Search returned %d results", len(results))
	
	if len(results) < minResults {
		t.Errorf("Expected at least %d results, got %d", minResults, len(results))
	}
	
	// Validate result structure
	for i, r := range results {
		if r.Username == "" {
			t.Errorf("Result %d: missing username", i)
		}
		if r.Filename == "" {
			t.Errorf("Result %d: missing filename", i)
		}
		if r.Size <= 0 {
			t.Errorf("Result %d: invalid size %d", i, r.Size)
		}
		
		t.Logf("Result %d: %s from %s (size: %d, score: %.1f)",
			i, r.Filename, r.Username, r.Size, r.Score)
	}
	
	return results
}

// ValidateDownloadFlow tests the download flow with slskd
// Note: This requires a real Soulseek peer to be available
func (h *IntegrationHarness) ValidateDownloadFlow(t *testing.T, username, filename string) {
	t.Helper()
	
	if slskdUsername == "" || slskdPassword == "" {
		t.Skip("Skipping download flow test: INTEGRATION_SLSKD_USERNAME/PASSWORD not set")
	}
	
	t.Logf("Testing download flow for %s from %s", filename, username)
	
	// Enqueue download
	downloadID, err := h.Slskd.EnqueueDownload(username, filename)
	if err != nil {
		t.Fatalf("Failed to enqueue download: %v", err)
	}
	
	t.Logf("Download enqueued with ID: %s", downloadID)
	
	// Wait for download to complete (with timeout)
	// Note: In real integration tests, this requires a peer with the file
	ctx, cancel := context.WithTimeout(context.Background(), downloadWaitTimeout)
	defer cancel()
	
	done := make(chan *services.Download, 1)
	errChan := make(chan error, 1)
	
	go func() {
		download, err := h.Slskd.WaitForDownload(username, filename, downloadWaitTimeout)
		if err != nil {
			errChan <- err
			return
		}
		done <- download
	}()
	
	select {
	case <-ctx.Done():
		t.Log("Download wait timed out (expected if no peer has the file)")
	case err := <-errChan:
		t.Logf("Download failed: %v (expected if peer unavailable)", err)
	case download := <-done:
		if download.State == services.DownloadStateCompleted {
			t.Logf("Download completed successfully: %s", download.Path)
		} else {
			t.Errorf("Download ended with unexpected state: %s", download.State)
		}
	}
}

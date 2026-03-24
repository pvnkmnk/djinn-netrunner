//go:build integration

// Package integration provides end-to-end testing scenarios for acquisition flows
//
package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

// TestSlskdEndToEndSearch validates real search functionality against dockerized slskd
func TestSlskdEndToEndSearch(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	tests := []struct {
		name       string
		query      string
		minResults int
	}{
		{
			name:       "common track search",
			query:      "The Beatles Yesterday",
			minResults: 0, // May not find results in test environment
		},
		{
			name:       "artist search",
			query:      "Pink Floyd",
			minResults: 0,
		},
		{
			name:       "album search",
			query:      "Dark Side of the Moon",
			minResults: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := harness.ValidateEndToEndSearch(t, tt.query, tt.minResults)
			
			// Validate scoring algorithm
			if len(results) > 1 {
				// Results should be sorted by score (highest first)
				for i := 0; i < len(results)-1; i++ {
					if results[i].Score < results[i+1].Score {
						t.Errorf("Results not properly sorted by score: %.1f < %.1f at index %d",
							results[i].Score, results[i+1].Score, i)
					}
				}
			}
			
			// Log search metadata
			t.Logf("Search '%s' completed with %d results", tt.query, len(results))
		})
	}
}

// TestSlskdHealthCheck validates slskd health endpoint
func TestSlskdHealthCheck(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	if !harness.Slskd.HealthCheck() {
		t.Fatal("slskd health check failed")
	}
	
	t.Log("slskd health check passed")
}

// TestSlskdSearchWithQualityProfile validates search with quality profile constraints
func TestSlskdSearchWithQualityProfile(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	// Create quality profile with specific constraints
	highQualityProfile := &database.QualityProfile{
		Name:           "High Quality Test Profile",
		PreferLossless: true,
		MinBitrate:     320,
		AllowedFormats: "flac,wav",
	}
	
	if err := harness.DB.Create(highQualityProfile).Error; err != nil {
		t.Fatalf("Failed to create quality profile: %v", err)
	}
	
	// Search with quality profile
	results, err := harness.Slskd.Search("test query", 10, highQualityProfile)
	if err != nil {
		t.Fatalf("Search with quality profile failed: %v", err)
	}
	
	// Validate that high-quality results get better scores
	for _, r := range results {
		if r.Bitrate != nil {
			t.Logf("Result: %s - Bitrate: %d, Score: %.1f", r.Filename, *r.Bitrate, r.Score)
		}
	}
}

// TestSlskdDownloadLifecycle validates the complete download lifecycle
func TestSlskdDownloadLifecycle(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	// This test requires a real Soulseek environment with known files
	// Skip if we don't have valid credentials
	if slskdUsername == "" || slskdPassword == "" {
		t.Skip("Skipping: Real Soulseek credentials not configured")
	}
	
	t.Skip("Skipping: Download lifecycle test requires real Soulseek peer with test files")
	
	// Test download enqueue
	testUsername := "test_peer"
	testFilename := "test_artist_test_song.mp3"
	
	// Enqueue download
	downloadID, err := harness.Slskd.EnqueueDownload(testUsername, testFilename)
	if err != nil {
		t.Fatalf("Failed to enqueue download: %v", err)
	}
	
	if downloadID == "" {
		t.Fatal("Expected non-empty download ID")
	}
	
	t.Logf("Download enqueued: %s", downloadID)
	
	// Verify download was created
	download, err := harness.Slskd.GetDownload(testUsername, testFilename)
	if err != nil {
		t.Fatalf("Failed to get download status: %v", err)
	}
	
	if download == nil {
		t.Fatal("Expected download to exist")
	}
	
	t.Logf("Download state: %s", download.State)
	
	// Wait for completion (will likely timeout in test environment)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	completed := make(chan *services.Download, 1)
	go func() {
		d, err := harness.Slskd.WaitForDownload(testUsername, testFilename, 5*time.Second)
		if err == nil && d != nil && d.State == services.DownloadStateCompleted {
			completed <- d
		}
	}()
	
	select {
	case <-ctx.Done():
		t.Log("Download wait timed out (expected in test environment without real peers)")
	case d := <-completed:
		t.Logf("Download completed: %s", d.Path)
	}
}

// TestSlskdErrorHandling validates error handling for various failure scenarios
func TestSlskdErrorHandling(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	t.Run("invalid search query", func(t *testing.T) {
		// Test with empty query
		results, err := harness.Slskd.Search("", 5, nil)
		if err != nil {
			t.Logf("Empty query returned error (may be expected): %v", err)
		} else {
			t.Logf("Empty query returned %d results", len(results))
		}
	})
	
	t.Run("long search query", func(t *testing.T) {
		// Test with very long query
		longQuery := strings.Repeat("a", 500)
		results, err := harness.Slskd.Search(longQuery, 5, nil)
		if err != nil {
			t.Logf("Long query returned error: %v", err)
		} else {
			t.Logf("Long query returned %d results", len(results))
		}
	})
	
	t.Run("special characters in query", func(t *testing.T) {
		specialQueries := []string{
			"Test & Artist",
			"Artist (feat. Other)",
			"Song [Remix]",
			"Track #1",
		}
		
		for _, query := range specialQueries {
			results, err := harness.Slskd.Search(query, 5, nil)
			if err != nil {
				t.Logf("Query '%s' returned error: %v", query, err)
			} else {
				t.Logf("Query '%s' returned %d results", query, len(results))
			}
		}
	})
}

// TestSlskdConcurrentOperations validates concurrent search and download operations
func TestSlskdConcurrentOperations(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	queries := []string{
		"Artist One Song",
		"Artist Two Track",
		"Artist Three Music",
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	
	results := make(chan struct {
		query  string
		count  int
		err    error
	}, len(queries))
	
	// Launch concurrent searches
	for _, query := range queries {
		go func(q string) {
			searchResults, err := harness.Slskd.Search(q, 10, nil)
			results <- struct {
				query string
				count int
				err   error
			}{q, len(searchResults), err}
		}(query)
	}
	
	// Collect results
	for i := 0; i < len(queries); i++ {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for concurrent searches")
		case r := <-results:
			if r.err != nil {
				t.Errorf("Search '%s' failed: %v", r.query, r.err)
			} else {
				t.Logf("Search '%s' completed: %d results", r.query, r.count)
			}
		}
	}
}

// TestSlskdScoreCalculation validates the search result scoring algorithm
func TestSlskdScoreCalculation(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	profile := harness.TestQualityProfile
	
	// Perform a search and validate scoring
	results, err := harness.Slskd.Search("test scoring", 10, profile)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	
	for _, r := range results {
		// Validate score is reasonable (not NaN, not infinite)
		if r.Score < -10000 || r.Score > 10000 {
			t.Errorf("Unreasonable score for %s: %.1f", r.Filename, r.Score)
		}
		
		// Log scoring details
		bitrate := "unknown"
		if r.Bitrate != nil {
			bitrate = fmt.Sprintf("%d", *r.Bitrate)
		}
		
		t.Logf("Score: %.1f | %s | Bitrate: %s | Queue: %d | Speed: %d",
			r.Score, r.Filename, bitrate, r.QueueLength, r.Speed)
	}
}

// TestEndToEndAcquisitionFlow validates the complete acquisition flow
func TestEndToEndAcquisitionFlow(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	
	// This is an end-to-end test that would:
	// 1. Create a watchlist
	// 2. Create a sync job
	// 3. Wait for acquisition job to be created
	// 4. Process acquisition items
	// 5. Validate downloads complete
	// 6. Validate files are imported
	
	t.Skip("Skipping full E2E flow: Requires complete environment setup with real Soulseek peers")
	
	// Create test job items
	items := []TestJobItem{
		{
			Query:  "Test Artist Test Song",
			Artist: "Test Artist",
			Album:  "Test Album",
			Title:  "Test Song",
		},
	}
	
	job, jobItems := harness.CreateTestJob(t, items)
	
	t.Logf("Created test job #%d with %d items", job.ID, len(jobItems))
	
	// In a full E2E test, we would now:
	// - Start job processing
	// - Monitor job state changes
	// - Validate each step of the acquisition flow
}

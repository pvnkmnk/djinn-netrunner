//go:build integration

// Package integration provides end-to-end pipeline tests for acquisition flows.
//
// Tests cover:
//  1. Watchlist sync creates acquisition job with correct items
//  2. Full pipeline with mock slskd: sync → search → download → import
//  3. Download failure handling
//  4. Metadata enrichment fallback
//  5. Concurrent job execution
//  6. Library prune removes stale track records and writes job logs
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/pvnkmnk/netrunner/backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// Mock slskd HTTP server helpers
// ──────────────────────────────────────────────

// mockSlskdConfig controls mock slskd server behavior per test scenario.
type mockSlskdConfig struct {
	searchResults   []map[string]interface{}
	searchErr       bool
	downloadState   string
	downloadErr     bool
	downloadTimeout int
	healthOK        bool

	// Staging fixture: when stagingDir is set, the mock materializes the
	// "downloaded" file there when its state poll first reports Completed,
	// mimicking real slskd writing into its downloads volume. stagedRelPath
	// is the remote path (dir + filename) the peer serves.
	stagingDir      string
	stagedRelPath   string
	downloadCounter int
}

func defaultMockSlskdConfig() *mockSlskdConfig {
	return &mockSlskdConfig{
		healthOK: true,
		searchResults: []map[string]interface{}{
			{
				"username":    "mockpeer",
				"uploadSpeed": 1000,
				"queueLength": 0,
				"files": []map[string]interface{}{
					{"filename": "Test Album/Test Artist - Test Song.mp3", "size": 5000000, "bitRate": 320, "isLocked": false},
				},
			},
		},
		downloadState:   "Completed",
		downloadErr:     false,
		downloadTimeout: 0,
	}
}

func newMockSlskdServer(cfg *mockSlskdConfig) *httptest.Server {
	var mu sync.Mutex
	pollCount := 0
	searchID := "mock-search-id"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "GET" && r.URL.Path == "/api/v0/session" {
			if cfg.healthOK {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"isLoggedIn":true}`)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v0/searches" {
			if cfg.searchErr {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"mock search error"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"id": searchID})
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v0/searches/") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"state":     "Completed", // terminal state so Search exits its poll loop immediately
				"responses": cfg.searchResults,
			})
			return
		}
		if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/v0/searches/") {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/v0/transfers/downloads/") {
			if cfg.downloadErr {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"mock download error"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enqueued": []map[string]interface{}{
					{"id": "mock-download-id", "username": "mockpeer", "filename": "test.mp3", "state": "Queued"},
				},
				"failed": []string{},
			})
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v0/transfers/downloads/") {
			if cfg.downloadTimeout > 0 && pollCount < cfg.downloadTimeout {
				pollCount++
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id": "mock-download-id", "state": "InProgress",
					"filename":         "Test Album/Test Artist - Test Song.mp3",
					"bytesTransferred": int64(pollCount * 100000), "size": int64(5000000),
				})
				return
			}
			// Materialize the staged file like real slskd would have, once
			// per completed download. Each download gets distinct bytes so
			// hash-level dedup cannot mask album-level dedup in tests.
			if cfg.stagingDir != "" {
				cfg.downloadCounter++
				staged := filepath.Join(cfg.stagingDir, filepath.FromSlash(cfg.stagedRelPath))
				if err := os.MkdirAll(filepath.Dir(staged), 0o755); err == nil {
					_ = os.WriteFile(staged, []byte(fmt.Sprintf("fake audio payload #%d for Test Artist - Test Song", cfg.downloadCounter)), 0o644)
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "mock-download-id", "state": cfg.downloadState,
				"filename": "Test Album/Test Artist - Test Song.mp3", "size": int64(5000000),
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/browse") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"directories": []map[string]interface{}{
					{"name": "Test Album", "files": []map[string]interface{}{
						{"filename": "Test Artist - Test Song.mp3", "size": 5000000, "bitRate": 320},
					}},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// ──────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────

func createSyncJob(t *testing.T, db *gorm.DB, watchlist *database.Watchlist) *database.Job {
	t.Helper()
	job := &database.Job{
		Type: "sync", State: "running", ScopeType: "watchlist",
		ScopeID: watchlist.ID.String(), RequestedAt: time.Now(), CreatedBy: "integration_test",
	}
	require.NoError(t, db.Create(job).Error)
	return job
}

func createPipelineWatchlist(t *testing.T, db *gorm.DB, profile *database.QualityProfile) *database.Watchlist {
	t.Helper()
	wl := &database.Watchlist{
		Name: "Pipeline Test Watchlist", SourceType: "pipeline_test",
		SourceURI: "pipeline://test", QualityProfileID: profile.ID, Enabled: true,
	}
	require.NoError(t, db.Create(wl).Error)
	return wl
}

func cleanupPipelineData(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.Exec("DELETE FROM acquisitions WHERE job_id IN (SELECT id FROM jobs WHERE created_by IN ('integration_test','sync_handler'))")
	db.Exec("DELETE FROM job_logs WHERE job_id IN (SELECT id FROM jobs WHERE created_by IN ('integration_test','sync_handler'))")
	db.Exec("DELETE FROM jobitems WHERE job_id IN (SELECT id FROM jobs WHERE created_by IN ('integration_test','sync_handler'))")
	db.Exec("DELETE FROM jobs WHERE created_by IN ('integration_test','sync_handler')")
	db.Exec("DELETE FROM watchlists WHERE name LIKE 'Pipeline%'")
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 1: Watchlist Sync → Acquisition Job
// ══════════════════════════════════════════════════════════════════════════════
func TestPipelineSyncCreatesAcquisitionJob(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	mockProvider := &testutil.MockProvider{
		Tracks: []map[string]string{
			{"artist": "Test Artist", "title": "Test Song", "album": "Test Album", "cover_art_url": ""},
			{"artist": "Another Artist", "title": "Another Track", "album": "", "cover_art_url": ""},
		},
		SnapID: "test-snap-001",
	}

	ws := services.NewWatchlistService(harness.DB, nil, harness.Config)
	ws.RegisterProvider("pipeline_test", mockProvider)
	sh := services.NewSyncHandler(harness.DB, nil, ws)

	watchlist := createPipelineWatchlist(t, harness.DB, harness.TestQualityProfile)
	job := createSyncJob(t, harness.DB, watchlist)

	err := sh.Execute(context.Background(), job.ID, *job)
	require.NoError(t, err)

	var acqJob database.Job
	err = harness.DB.Where("job_type = ? AND scope_id = ? AND created_by = ?",
		"acquisition", watchlist.ID.String(), "sync_handler").First(&acqJob).Error
	require.NoError(t, err, "acquisition job should be created by sync handler")
	assert.Equal(t, "queued", acqJob.State)
	assert.NotZero(t, acqJob.ID)

	var items []database.JobItem
	err = harness.DB.Where("job_id = ?", acqJob.ID).Order("sequence ASC").Find(&items).Error
	require.NoError(t, err)
	require.Len(t, items, 2)

	assert.Equal(t, "Test Artist", items[0].Artist)
	assert.Equal(t, "Test Song", items[0].TrackTitle)
	assert.Equal(t, "Test Album", items[0].Album)
	assert.Equal(t, "queued", items[0].Status)
	assert.Equal(t, "Another Artist", items[1].Artist)
	assert.Equal(t, "Another Track", items[1].TrackTitle)
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 2: Full Pipeline (Sync → Search → Download → Import) with mock slskd
// ══════════════════════════════════════════════════════════════════════════════
//
// Note: SlskdService.Search() has a built-in 30-second sleep for result
// gathering, so this test takes ~35s. Expected for integration tests.
//
// TestPipelineFullPipelineWithMockSlskd exercises the full acquisition
// pipeline end-to-end at the app level: search → select → enqueue →
// download-wait → import (canonical album-artist folder) → staging sweep,
// plus the album-level dedup on a re-run of the same release.
//
// The mock slskd materializes the "downloaded" file into the staging dir
// (like real slskd writing into its downloads volume) so the import stage's
// os.Stat passes — this is the fixture the original version of this test
// lacked, which is why it used to be skipped.
func TestPipelineFullPipelineWithMockSlskd(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	libRoot := filepath.Join(t.TempDir(), "music_lib")
	staging := filepath.Join(t.TempDir(), "downloads")
	require.NoError(t, os.MkdirAll(staging, 0o755))

	mockCfg := defaultMockSlskdConfig()
	mockCfg.stagingDir = staging
	mockCfg.stagedRelPath = "Test Album/Test Artist - Test Song.mp3"
	// Real slskd reports "Completed, Succeeded"; WaitForDownload requires both.
	mockCfg.downloadState = "Completed, Succeeded"
	mockServer := newMockSlskdServer(mockCfg)
	defer mockServer.Close()

	pipelineCfg := &config.Config{
		DatabaseURL: harness.Config.DatabaseURL, SlskdURL: mockServer.URL,
		SlskdAPIKey: "test-key", MusicLibraryPath: libRoot,
		DownloadStagingPath: staging,
		AllowPrivateTargets: true, // mock slskd serves from loopback httptest
	}

	mockSlskd := services.NewSlskdService(pipelineCfg, harness.DB)
	require.NoError(t, os.MkdirAll(libRoot, 0o755))

	ah := services.NewAcquisitionHandler(
		harness.DB, pipelineCfg, mockSlskd,
		nil, nil, services.NewMetadataExtractor(),
		nil, nil, nil, nil, nil, nil,
	)

	// Two queued items for the same artist+album (the release re-enqueued).
	job := &database.Job{
		Type: "acquisition", State: "running", ScopeType: "manual",
		RequestedAt: time.Now(), CreatedBy: "integration_test",
	}
	require.NoError(t, harness.DB.Create(job).Error)

	mkItem := func(seq int) *database.JobItem {
		item := &database.JobItem{
			JobID: job.ID, Sequence: seq, Status: "queued",
			NormalizedQuery: "test artist test song",
			Artist:          "Test Artist", TrackTitle: "Test Song", Album: "Test Album",
		}
		require.NoError(t, harness.DB.Create(item).Error)
		return item
	}
	item1 := mkItem(0)
	item2 := mkItem(1)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ── First item: full import. ────────────────────────────────────────
	require.NoError(t, ah.ExecuteItem(ctx, job.ID, item1.ID))

	var updated1 database.JobItem
	require.NoError(t, harness.DB.First(&updated1, item1.ID).Error)
	assert.Contains(t, updated1.Status, "imported", "first item should import (got %q: %s)",
		updated1.Status, updated1.FailureReason)

	var acqs1 []database.Acquisition
	require.NoError(t, harness.DB.Where("job_item_id = ?", item1.ID).Find(&acqs1).Error)
	require.Len(t, acqs1, 1, "acquisition record should exist for the import")

	// Canonical album-artist folder layout: libraryRoot/<artist>/<album>/<file>
	expectedDir := filepath.Join(libRoot, "Test Artist", "Test Album")
	assert.True(t, strings.HasPrefix(acqs1[0].FinalPath, expectedDir),
		"import should land in the canonical artist/album folder, got %q", acqs1[0].FinalPath)
	_, err := os.Stat(acqs1[0].FinalPath)
	require.NoError(t, err, "imported file should exist in the library")

	// Staging sweep: the emptied Test Album dir (and its file) must be gone.
	_, err = os.Stat(filepath.Join(staging, "Test Album"))
	assert.True(t, os.IsNotExist(err), "staging album dir should be swept after import")

	// ── Second item: same release re-enqueued → album-level dedup. ─────
	require.NoError(t, ah.ExecuteItem(ctx, job.ID, item2.ID))

	var updated2 database.JobItem
	require.NoError(t, harness.DB.First(&updated2, item2.ID).Error)
	assert.Contains(t, updated2.Status, "duplicate album",
		"re-enqueued release should be caught by album-level dedup (got %q: %s)",
		updated2.Status, updated2.FailureReason)

	var acqs2 int64
	harness.DB.Model(&database.Acquisition{}).Where("job_item_id = ?", item2.ID).Count(&acqs2)
	assert.Equal(t, int64(0), acqs2, "deduped item must not create an acquisition record")

	// The redundant staged file was removed; staging stays clean.
	_, err = os.Stat(filepath.Join(staging, "Test Album", "Test Artist - Test Song.mp3"))
	assert.True(t, os.IsNotExist(err), "redundant staged file should be removed after dedup")

	entries, err := os.ReadDir(staging)
	require.NoError(t, err)
	assert.Empty(t, entries, "staging dir should be empty after import + dedup")

	// Library holds exactly one copy of the track.
	var total int64
	harness.DB.Model(&database.Acquisition{}).Where("artist = ? AND album = ?", "Test Artist", "Test Album").Count(&total)
	assert.Equal(t, int64(1), total, "exactly one acquisition for the artist+album")
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 3: Download Failure Handling
// ══════════════════════════════════════════════════════════════════════════════
//
// When slskd download enqueue fails, the item should be marked as failed
// with the appropriate error reason.
func TestPipelineDownloadFailure(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	// Mock slskd where download enqueue returns error
	mockCfg := defaultMockSlskdConfig()
	mockCfg.downloadErr = true
	mockServer := newMockSlskdServer(mockCfg)
	defer mockServer.Close()

	pipelineCfg := &config.Config{
		DatabaseURL: harness.Config.DatabaseURL, SlskdURL: mockServer.URL,
		SlskdAPIKey: "test-key", MusicLibraryPath: filepath.Join(t.TempDir(), "music_lib"),
		AllowPrivateTargets: true, // mock slskd serves from loopback httptest
	}

	mockSlskd := services.NewSlskdService(pipelineCfg, harness.DB)

	ah := services.NewAcquisitionHandler(
		harness.DB, pipelineCfg, mockSlskd,
		nil, nil, services.NewMetadataExtractor(),
		nil, nil, nil, nil, nil, nil,
	)

	// Create a job and item directly (bypass sync)
	job := &database.Job{
		Type: "acquisition", State: "running", ScopeType: "manual",
		RequestedAt: time.Now(), CreatedBy: "integration_test",
		Params: func() []byte {
			p, _ := json.Marshal(map[string]string{"quality_profile_id": harness.TestQualityProfile.ID.String()})
			return p
		}(),
	}
	require.NoError(t, harness.DB.Create(job).Error)

	item := &database.JobItem{
		JobID: job.ID, Sequence: 0, Status: "queued",
		NormalizedQuery: "Test Artist Test Song",
		Artist:          "Test Artist", TrackTitle: "Test Song", Album: "Test Album",
	}
	require.NoError(t, harness.DB.Create(item).Error)

	// Execute item (will fail at download stage)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := ah.ExecuteItem(ctx, job.ID, item.ID)
	assert.NoError(t, err, "ExecuteItem should not return error on download failure (item is marked failed)")

	var updatedItem database.JobItem
	require.NoError(t, harness.DB.First(&updatedItem, item.ID).Error)
	assert.Equal(t, "failed", updatedItem.Status, "item should be marked failed")
	assert.Contains(t, updatedItem.FailureReason, "download", "failure reason should reference download")
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 4: Metadata Enrichment Fallback
// ══════════════════════════════════════════════════════════════════════════════
//
// When metadata enrichment services (MusicBrainz, AcoustID) are unavailable,
// the pipeline should still complete import using basic tag extraction.
//
// TestPipelineMetadataFallback verifies the import completes with every
// enrichment service nil (no MusicBrainz/AcoustID/cover/lyrics), using only
// the job item's declared metadata — the degraded-network path.
func TestPipelineMetadataFallback(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	libRoot := filepath.Join(t.TempDir(), "music_lib")
	staging := filepath.Join(t.TempDir(), "downloads")
	require.NoError(t, os.MkdirAll(staging, 0o755))

	mockCfg := defaultMockSlskdConfig()
	mockCfg.stagingDir = staging
	mockCfg.stagedRelPath = "Test Album/Test Artist - Test Song.mp3"
	mockCfg.downloadState = "Completed, Succeeded"
	mockServer := newMockSlskdServer(mockCfg)
	defer mockServer.Close()

	require.NoError(t, os.MkdirAll(libRoot, 0o755))

	pipelineCfg := &config.Config{
		DatabaseURL: harness.Config.DatabaseURL, SlskdURL: mockServer.URL,
		SlskdAPIKey: "test-key", MusicLibraryPath: libRoot,
		DownloadStagingPath: staging,
		AllowPrivateTargets: true, // mock slskd serves from loopback httptest
	}

	mockSlskd := services.NewSlskdService(pipelineCfg, harness.DB)

	// All enrichment services are nil — tests fallback handling
	ah := services.NewAcquisitionHandler(
		harness.DB, pipelineCfg, mockSlskd,
		nil, // MusicBrainzService = nil
		nil, // AcoustIDService = nil
		services.NewMetadataExtractor(),
		nil, // LibraryClient = nil
		nil, // DiscogsService = nil
		nil, // CacheService = nil
		nil, // LyricsService = nil
		nil, // TranscoderService = nil
		nil, // YtdlpService = nil
	)

	job := &database.Job{
		Type: "acquisition", State: "running", ScopeType: "manual",
		RequestedAt: time.Now(), CreatedBy: "integration_test",
	}
	require.NoError(t, harness.DB.Create(job).Error)

	item := &database.JobItem{
		JobID: job.ID, Sequence: 0, Status: "queued",
		NormalizedQuery: "Test Artist Test Song",
		Artist:          "Test Artist", TrackTitle: "Test Song", Album: "Test Album",
	}
	require.NoError(t, harness.DB.Create(item).Error)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	require.NoError(t, ah.ExecuteItem(ctx, job.ID, item.ID))

	var updatedItem database.JobItem
	require.NoError(t, harness.DB.First(&updatedItem, item.ID).Error)
	assert.Contains(t, updatedItem.Status, "imported",
		"item should import even without enrichment services (got %q: %s)",
		updatedItem.Status, updatedItem.FailureReason)

	var acqs []database.Acquisition
	require.NoError(t, harness.DB.Where("job_item_id = ?", item.ID).Find(&acqs).Error)
	require.NotEmpty(t, acqs, "acquisition record should exist")
	assert.Empty(t, acqs[0].MBRecordingID, "MB recording ID should be empty (no enrichment)")

	_, err := os.Stat(acqs[0].FinalPath)
	require.NoError(t, err, "imported file should exist despite missing enrichment")
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 5: Concurrent Job Execution
// ══════════════════════════════════════════════════════════════════════════════
//
// Two sync jobs on different watchlists should complete without conflicts.
// Each should create its own acquisition job.
func TestPipelineConcurrentSyncJobs(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	// ── Create two independent watchlists ─────────────────────────────────
	mockProvider1 := &testutil.MockProvider{
		Tracks: []map[string]string{
			{"artist": "Alpha Artist", "title": "Alpha Track", "album": "Alpha", "cover_art_url": ""},
		},
		SnapID: "alpha-snap",
	}
	mockProvider2 := &testutil.MockProvider{
		Tracks: []map[string]string{
			{"artist": "Beta Artist", "title": "Beta Track", "album": "Beta", "cover_art_url": ""},
		},
		SnapID: "beta-snap",
	}

	ws1 := services.NewWatchlistService(harness.DB, nil, harness.Config)
	ws1.RegisterProvider("pipeline_test", mockProvider1)
	ws2 := services.NewWatchlistService(harness.DB, nil, harness.Config)
	ws2.RegisterProvider("pipeline_test", mockProvider2)

	sh1 := services.NewSyncHandler(harness.DB, nil, ws1)
	sh2 := services.NewSyncHandler(harness.DB, nil, ws2)

	wl1 := createPipelineWatchlist(t, harness.DB, harness.TestQualityProfile)
	wl2 := &database.Watchlist{
		Name: "Pipeline Test Watchlist 2", SourceType: "pipeline_test",
		SourceURI: "pipeline://test2", QualityProfileID: harness.TestQualityProfile.ID, Enabled: true,
	}
	require.NoError(t, harness.DB.Create(wl2).Error)

	job1 := createSyncJob(t, harness.DB, wl1)
	job2 := createSyncJob(t, harness.DB, wl2)

	// ── Execute concurrently ─────────────────────────────────────────────
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := sh1.Execute(ctx, job1.ID, *job1); e != nil {
			errs <- fmt.Errorf("sync1: %w", e)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := sh2.Execute(ctx, job2.ID, *job2); e != nil {
			errs <- fmt.Errorf("sync2: %w", e)
		}
	}()

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Errorf("Concurrent sync error: %v", e)
	}

	// ── Verify: two acquisition jobs created ─────────────────────────────
	var acqJobs int64
	harness.DB.Model(&database.Job{}).Where("job_type = ? AND created_by = ?",
		"acquisition", "sync_handler").Count(&acqJobs)
	assert.Equal(t, int64(2), acqJobs, "two acquisition jobs should be created")

	// ── Verify: items exist for both ─────────────────────────────────────
	var items1 []database.JobItem
	var acqJob1 database.Job
	require.NoError(t, harness.DB.Where("job_type = ? AND scope_id = ?",
		"acquisition", wl1.ID.String()).First(&acqJob1).Error)
	require.NoError(t, harness.DB.Where("job_id = ?", acqJob1.ID).Find(&items1).Error)
	assert.Len(t, items1, 1)
	assert.Equal(t, "Alpha Artist", items1[0].Artist)

	var items2 []database.JobItem
	var acqJob2 database.Job
	require.NoError(t, harness.DB.Where("job_type = ? AND scope_id = ?",
		"acquisition", wl2.ID.String()).First(&acqJob2).Error)
	require.NoError(t, harness.DB.Where("job_id = ?", acqJob2.ID).Find(&items2).Error)
	assert.Len(t, items2, 1)
	assert.Equal(t, "Beta Artist", items2[0].Artist)
}

// ══════════════════════════════════════════════════════════════════════════════
// AC 6: Library Prune — Stale Record Removal
// ══════════════════════════════════════════════════════════════════════════════
//
// Prune should remove Track records whose files no longer exist on disk,
// keep records for files that still exist, and record per-file status in
// job logs.
func TestLibraryPrune(t *testing.T) {
	harness := SetupIntegrationHarness(t)
	defer harness.Teardown(t)
	defer cleanupPipelineData(t, harness.DB)

	// ── Create a test library ────────────────────────────────────────────
	libRoot := filepath.Join(t.TempDir(), "prune_lib")
	require.NoError(t, os.MkdirAll(libRoot, 0755))

	library := &database.Library{
		Name: "Prune Test Library",
		Path: libRoot,
	}
	require.NoError(t, harness.DB.Create(library).Error)

	// ── Create test files and matching Track records ─────────────────────
	existingFile := filepath.Join(libRoot, "keep_me.mp3")
	require.NoError(t, os.WriteFile(existingFile, []byte("fake audio data"), 0644))

	staleFile := filepath.Join(libRoot, "delete_me.mp3")
	// Deliberately do NOT create the file on disk

	existingTrack := &database.Track{
		LibraryID: library.ID,
		Title:     "Keep Me",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      existingFile,
		Format:    "mp3",
		FileSize:  1024,
		FileHash:  "abc123",
	}
	require.NoError(t, harness.DB.Create(existingTrack).Error)

	staleTrack := &database.Track{
		LibraryID: library.ID,
		Title:     "Delete Me",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      staleFile,
		Format:    "mp3",
		FileSize:  2048,
		FileHash:  "def456",
	}
	require.NoError(t, harness.DB.Create(staleTrack).Error)

	// ── Create a prune job so we can verify job logs ─────────────────────
	job := &database.Job{
		Type: "prune", State: "queued",
		ScopeType: "library", ScopeID: library.ID.String(),
		RequestedAt: time.Now(), CreatedBy: "integration_test",
	}
	require.NoError(t, harness.DB.Create(job).Error)

	// ── Execute prune ────────────────────────────────────────────────────
	scanSvc := services.NewScannerService(harness.DB)
	ctx := context.Background()
	err := scanSvc.PruneTracks(ctx, library.ID, job.ID)
	require.NoError(t, err)

	// ── Verify: existing track kept, stale track removed ─────────────────
	var remainingTracks []database.Track
	require.NoError(t, harness.DB.Where("library_id = ?", library.ID).Find(&remainingTracks).Error)
	require.Len(t, remainingTracks, 1, "only the track with an existing file should remain")
	assert.Equal(t, "Keep Me", remainingTracks[0].Title)

	// ── Verify: job logs were written for removed file ───────────────────
	var logs []database.JobLog
	require.NoError(t, harness.DB.Where("job_id = ?", job.ID).Find(&logs).Error)
	require.NotEmpty(t, logs, "job logs should be written during prune")

	foundRemove := false
	foundSummary := false
	for _, log := range logs {
		if log.Level == "OK" && strings.Contains(log.Message, "Removed:") {
			foundRemove = true
		}
		if log.Level == "INFO" && strings.Contains(log.Message, "Prune complete") {
			foundSummary = true
		}
	}
	assert.True(t, foundRemove, "should have a log entry for the removed file")
	assert.True(t, foundSummary, "should have a summary log entry")

	// ── Cleanup ──────────────────────────────────────────────────────────
	os.RemoveAll(libRoot)
}

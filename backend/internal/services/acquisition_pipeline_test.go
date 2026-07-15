package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupPipelineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err, "failed to connect to db")
	err = database.Migrate(db)
	require.NoError(t, err, "failed to migrate db")
	sqlDB, err := db.DB()
	require.NoError(t, err, "failed to get underlying sql.DB")
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

// ---------------------------------------------------------------------------
// Mock implementations for testing
// ---------------------------------------------------------------------------

type mockSlskd struct {
	SearchFunc           func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error)
	BrowseFunc           func(username string) ([]PeerFile, error)
	EnqueueDownloadFunc  func(username, filename string, size int64) (string, error)
	WaitForDownloadFunc  func(ctx context.Context, username, downloadID string, timeout time.Duration) (*Download, error)
}

func (m *mockSlskd) Search(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
	if m.SearchFunc != nil {
		return m.SearchFunc(query, timeout, profile)
	}
	return nil, nil
}

func (m *mockSlskd) Browse(username string) ([]PeerFile, error) {
	if m.BrowseFunc != nil {
		return m.BrowseFunc(username)
	}
	return nil, nil
}

func (m *mockSlskd) EnqueueDownload(username, filename string, size int64) (string, error) {
	if m.EnqueueDownloadFunc != nil {
		return m.EnqueueDownloadFunc(username, filename, size)
	}
	return "", nil
}

func (m *mockSlskd) WaitForDownload(ctx context.Context, username, downloadID string, timeout time.Duration) (*Download, error) {
	if m.WaitForDownloadFunc != nil {
		return m.WaitForDownloadFunc(ctx, username, downloadID, timeout)
	}
	return nil, nil
}

type mockGonic struct {
	Search3Func    func(query string) ([]GonicSong, error)
	TriggerScanFunc func() (bool, error)
}

func (m *mockGonic) Search3(query string) ([]GonicSong, error) {
	if m.Search3Func != nil {
		return m.Search3Func(query)
	}
	return nil, nil
}

func (m *mockGonic) TriggerScan() (bool, error) {
	if m.TriggerScanFunc != nil {
		return m.TriggerScanFunc()
	}
	return false, nil
}

type mockNavidrome struct {
	Search3Func    func(query string) ([]NavidromeSong, error)
	TriggerScanFunc func() (bool, error)
}

func (m *mockNavidrome) Search3(query string) ([]NavidromeSong, error) {
	if m.Search3Func != nil {
		return m.Search3Func(query)
	}
	return nil, nil
}

func (m *mockNavidrome) TriggerScan() (bool, error) {
	if m.TriggerScanFunc != nil {
		return m.TriggerScanFunc()
	}
	return false, nil
}

type mockYtdlp struct {
	DownloadAudioFunc   func(rawURL, outputDir, audioFormat string) (string, error)
	IsYtdlpAvailableFunc func() bool
}

func (m *mockYtdlp) DownloadAudio(rawURL, outputDir, audioFormat string) (string, error) {
	if m.DownloadAudioFunc != nil {
		return m.DownloadAudioFunc(rawURL, outputDir, audioFormat)
	}
	return "", nil
}

func (m *mockYtdlp) IsYtdlpAvailable() bool {
	if m.IsYtdlpAvailableFunc != nil {
		return m.IsYtdlpAvailableFunc()
	}
	return false
}

// ---------------------------------------------------------------------------
// stageSelectBestResult tests (pure logic)
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageSelectBestResult_NoProfile(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	bitrate := 320
	p := &acquisitionPipeline{
		item: database.JobItem{JobID: 1, ID: 1},
		results: []SearchResult{
			{Filename: "track1.mp3", Score: 50.0, Bitrate: &bitrate},
			{Filename: "track2.flac", Score: 40.0},
		},
	}

	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (always continues)")
	}
	if p.best.Filename != "track1.mp3" {
		t.Errorf("expected best=track1.mp3 (first by score), got %s", p.best.Filename)
	}
	if p.best.Score != 50.0 {
		t.Errorf("expected best.Score=50.0, got %f", p.best.Score)
	}
}

func TestAcquisitionHandler_StageSelectBestResult_WithProfile_Matching(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	profile := &database.QualityProfile{
		AllowedFormats: "flac",
		PreferLossless: true,
	}

	bitrateFLAC := 1000
	bitrateMP3 := 320
	p := &acquisitionPipeline{
		item: database.JobItem{JobID: 1, ID: 1},
		profile: profile,
		results: []SearchResult{
			{Filename: "track1.mp3", Score: 60.0, Bitrate: &bitrateMP3},
			{Filename: "track2.flac", Score: 50.0, Bitrate: &bitrateFLAC},
		},
	}

	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false")
	}
	// stageSelectBestResult always picks results[0] regardless of profile match
	if p.best.Filename != "track1.mp3" {
		t.Errorf("expected best=track1.mp3 (always first), got %s", p.best.Filename)
	}
}

func TestAcquisitionHandler_StageSelectBestResult_WithProfile_NonMatching(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	profile := &database.QualityProfile{
		AllowedFormats: "flac",
		PreferLossless: true,
	}

	bitrateMP3 := 320
	p := &acquisitionPipeline{
		item: database.JobItem{JobID: 1, ID: 1},
		profile: profile,
		results: []SearchResult{
			{Filename: "track1.mp3", Score: 50.0, Bitrate: &bitrateMP3}, // doesn't match profile
		},
	}

	// Should NOT return error or skip - it logs a warning but continues
	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (continues even when profile doesn't match)")
	}
	if p.best.Filename != "track1.mp3" {
		t.Errorf("expected best=track1.mp3, got %s", p.best.Filename)
	}
}

func TestAcquisitionHandler_StageSelectBestResult_NoResults(t *testing.T) {
	// stageSelectBestResult panics on p.results[0] with empty results.
	// The caller (stageSearchSoulseek) fails the item if no results are found,
	// so this case is tested via stageSearchSoulseek tests instead.
	// We verify here that the assumption holds: calling with empty results panics.
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	p := &acquisitionPipeline{
		item:    database.JobItem{JobID: 1, ID: 1},
		results: []SearchResult{},
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on empty results")
			}
		}()
		handler.stageSelectBestResult(p)
	}()
}

// ---------------------------------------------------------------------------
// stageSearchSoulseek tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageSearchSoulseek_Success(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			require.Equal(t, "test track", query, "search should be called with expected query")
			return []SearchResult{
				{Filename: "track1.mp3", Score: 50.0},
				{Filename: "track2.flac", Score: 40.0},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item, job: job}
	skip, err := handler.stageSearchSoulseek(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (results found)")
	}
	if len(p.results) != 2 {
		t.Errorf("expected 2 results, got %d", len(p.results))
	}
	if p.results[0].Filename != "track1.mp3" {
		t.Errorf("expected first result track1.mp3, got %s", p.results[0].Filename)
	}
}

func TestAcquisitionHandler_StageSearchSoulseek_NoResults(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			return []SearchResult{}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item, job: job}
	skip, err := handler.stageSearchSoulseek(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (no results)")
	}
	if len(p.results) != 0 {
		t.Errorf("expected 0 results, got %d", len(p.results))
	}

	// Verify item was failed
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "failed" {
		t.Errorf("expected status 'failed', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageSearchSoulseek_Error(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			return nil, errors.New("network error")
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item, job: job}
	skip, err := handler.stageSearchSoulseek(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (error)")
	}
}

// ---------------------------------------------------------------------------
// stageCheckGonicIndex tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageCheckGonicIndex_GonicMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	gonicMock := &mockGonic{
		Search3Func: func(query string) ([]GonicSong, error) {
			return []GonicSong{
				{ID: "123", Title: "Test Track", Artist: "Test Artist"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, gonicMock, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckGonicIndex(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (found in Gonic)")
	}

	// Verify item was marked as completed
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "completed (already indexed)" {
		t.Errorf("expected status 'completed (already indexed)', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageCheckGonicIndex_GonicNoMatch_NavidromeMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	gonicMock := &mockGonic{
		Search3Func: func(query string) ([]GonicSong, error) {
			return []GonicSong{}, nil // no match
		},
	}

	navidromeMock := &mockNavidrome{
		Search3Func: func(query string) ([]NavidromeSong, error) {
			return []NavidromeSong{
				{ID: "456", Title: "Test Track", Artist: "Test Artist"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, gonicMock, navidromeMock, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckGonicIndex(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (found in Navidrome)")
	}

	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "completed (already indexed)" {
		t.Errorf("expected status 'completed (already indexed)', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageCheckGonicIndex_BothClientsNil(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckGonicIndex(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (both clients nil, should continue)")
	}
}

func TestAcquisitionHandler_StageCheckGonicIndex_NoMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	gonicMock := &mockGonic{
		Search3Func: func(query string) ([]GonicSong, error) {
			return []GonicSong{}, nil
		},
	}

	navidromeMock := &mockNavidrome{
		Search3Func: func(query string) ([]NavidromeSong, error) {
			return []NavidromeSong{}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, gonicMock, navidromeMock, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckGonicIndex(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (not found in any index)")
	}
}

// ---------------------------------------------------------------------------
// stageYtdlpFallback tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageYtdlpFallback_Success(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(rawURL, outputDir, audioFormat string) (string, error) {
			if rawURL == "https://youtube.com/watch?v=test" {
				return "/downloads/test.flac", nil
			}
			return "", errors.New("unexpected URL")
		},
	}

	cfg := &config.Config{DownloadStagingPath: "/downloads"}
	handler := NewAcquisitionHandler(db, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	downloaded, ok := handler.stageYtdlpFallback(context.Background(), p)
	if !ok {
		t.Error("expected ok=true (download succeeded)")
	}
	if downloaded != "/downloads/test.flac" {
		t.Errorf("expected downloaded='/downloads/test.flac', got '%s'", downloaded)
	}

	// Verify item status was reset
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "downloading" {
		t.Errorf("expected status 'downloading', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageYtdlpFallback_NoSourceURL(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(rawURL, outputDir, audioFormat string) (string, error) {
			t.Error("DownloadAudio should not be called when SourceURL is empty")
			return "", nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	downloaded, ok := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (no SourceURL)")
	}
	if downloaded != "" {
		t.Errorf("expected downloaded='', got '%s'", downloaded)
	}
}

func TestAcquisitionHandler_StageYtdlpFallback_YtdlpUnavailable(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return false },
		DownloadAudioFunc: func(rawURL, outputDir, audioFormat string) (string, error) {
			t.Error("DownloadAudio should not be called when ytdlp unavailable")
			return "", nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (ytdlp unavailable)")
	}
}

func TestAcquisitionHandler_StageYtdlpFallback_DownloadError(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(rawURL, outputDir, audioFormat string) (string, error) {
			return "", errors.New("download failed")
		},
	}

	cfg := &config.Config{DownloadStagingPath: "/downloads"}
	handler := NewAcquisitionHandler(db, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (download error)")
	}
}

func TestAcquisitionHandler_StageYtdlpFallback_YtdlpNil(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (ytdlp nil)")
	}
}

// ---------------------------------------------------------------------------
// Execute trigger-scan tests
// Note: Execute tests are timing-sensitive due to the 5-second polling interval.
// We only test immediate-exit cases here (0 items, context cancellation).
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_Execute_ContextCancelledWhilePolling(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	// Create item that's "in progress" (not completed/failed)
	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	// Use a timeout context - will cancel after a brief wait
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := handler.Execute(ctx, job.ID, job)
	require.Error(t, err, "expected error")
	require.Equal(t, context.DeadlineExceeded, err, "expected DeadlineExceeded")
}

// ---------------------------------------------------------------------------
// stageAlbumBrowse tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageAlbumBrowse_SingleTrack(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		BrowseFunc: func(username string) ([]PeerFile, error) {
			return []PeerFile{
				{Filename: "/music/track1.flac"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track1", Artist: "Test Artist", Album: "Test Album", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	bitrate := 1000
	p := &acquisitionPipeline{
		item: item,
		best: SearchResult{Username: "peer1", Filename: "/music/track1.flac", Bitrate: &bitrate},
	}

	handler.stageAlbumBrowse(p)

	// Single track - no additional items created
	var newItems []database.JobItem
	require.NoError(t, db.Where("job_id = ? AND id != ?", job.ID, item.ID).Find(&newItems).Error, "failed to find new items")
	if len(newItems) != 0 {
		t.Errorf("expected 0 new items, got %d", len(newItems))
	}
}

func TestAcquisitionHandler_StageAlbumBrowse_MultipleTracks_CreatesJobItems(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		BrowseFunc: func(username string) ([]PeerFile, error) {
			return []PeerFile{
				{Filename: "/music/Album/track1.flac"},
				{Filename: "/music/Album/track2.flac"},
				{Filename: "/music/Album/track3.flac"},
				{Filename: "/music/Other/track4.flac"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track1", Artist: "Test Artist", Album: "Test Album", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	bitrate := 1000
	p := &acquisitionPipeline{
		item: item,
		best: SearchResult{Username: "peer1", Filename: "/music/Album/track1.flac", Bitrate: &bitrate},
	}

	handler.stageAlbumBrowse(p)

	// Should create 2 new items (track2 and track3, track1 is already in existingQueries)
	var newItems []database.JobItem
	require.NoError(t, db.Where("job_id = ? AND id != ?", job.ID, item.ID).Find(&newItems).Error, "failed to find new items")
	if len(newItems) != 2 {
		t.Errorf("expected 2 new items, got %d", len(newItems))
	}

	// Verify p.albumFiles was set
	if len(p.albumFiles) != 3 {
		t.Errorf("expected 3 album files, got %d", len(p.albumFiles))
	}
}

func TestAcquisitionHandler_StageAlbumBrowse_DeduplicatesAgainstExisting(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		BrowseFunc: func(username string) ([]PeerFile, error) {
			return []PeerFile{
				{Filename: "/music/Album/track1.flac"},
				{Filename: "/music/Album/track2.flac"},
				{Filename: "/music/Album/track3.flac"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	// Create existing item for track2 (dedup should prevent duplicate)
	existingItem := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track2", Artist: "Test Artist", Album: "Test Album", Sequence: 1}
	require.NoError(t, db.Create(&existingItem).Error, "failed to create existing item")

	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track1", Artist: "Test Artist", Album: "Test Album", Sequence: 2}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	bitrate := 1000
	p := &acquisitionPipeline{
		item: item,
		best: SearchResult{Username: "peer1", Filename: "/music/Album/track1.flac", Bitrate: &bitrate},
	}

	handler.stageAlbumBrowse(p)

	// Should only create 1 new item (track3, since track1 and track2 are in existing queries)
	var newItems []database.JobItem
	require.NoError(t, db.Where("job_id = ? AND id NOT IN (?, ?)", job.ID, item.ID, existingItem.ID).Find(&newItems).Error, "failed to find new items")
	if len(newItems) != 1 {
		t.Errorf("expected 1 new item (track3), got %d", len(newItems))
	}
}

func TestAcquisitionHandler_StageAlbumBrowse_BrowseError(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		BrowseFunc: func(username string) ([]PeerFile, error) {
			return nil, errors.New("browse failed")
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track1", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	bitrate := 1000
	p := &acquisitionPipeline{
		item: item,
		best: SearchResult{Username: "peer1", Filename: "/music/track1.flac", Bitrate: &bitrate},
	}

	// Should not panic - browse error is logged and ignored
	handler.stageAlbumBrowse(p)

	if len(p.albumFiles) != 0 {
		t.Errorf("expected 0 album files after browse error, got %d", len(p.albumFiles))
	}
}

func TestAcquisitionHandler_StageAlbumBrowse_NoAudioFiles(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		BrowseFunc: func(username string) ([]PeerFile, error) {
			return []PeerFile{
				{Filename: "/music/Album/cover.jpg"},
				{Filename: "/music/Album/info.txt"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "track1", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	bitrate := 1000
	p := &acquisitionPipeline{
		item: item,
		best: SearchResult{Username: "peer1", Filename: "/music/Album/track1.flac", Bitrate: &bitrate},
	}

	handler.stageAlbumBrowse(p)

	// No audio files found - albumFiles should be empty
	if len(p.albumFiles) != 0 {
		t.Errorf("expected 0 album files (no audio), got %d", len(p.albumFiles))
	}
}

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
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
	WaitForDownloadFunc  func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error)
	CancelDownloadFunc   func(username, downloadID string) error
	LocalPathForFunc     func(username, filename string) string
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

func (m *mockSlskd) WaitForDownload(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
	if m.WaitForDownloadFunc != nil {
		return m.WaitForDownloadFunc(ctx, username, downloadID, opts)
	}
	return nil, nil
}

// LocalPathFor defaults to the empty string, which is also the "no staged
// path recorded" state: a test that does not care about ownership behaves
// exactly as before the path was recorded.
func (m *mockSlskd) LocalPathFor(username, filename string) string {
	if m.LocalPathForFunc != nil {
		return m.LocalPathForFunc(username, filename)
	}
	return ""
}

func (m *mockSlskd) CancelDownload(username, downloadID string) error {
	if m.CancelDownloadFunc != nil {
		return m.CancelDownloadFunc(username, downloadID)
	}
	return nil
}

type mockLibrary struct {
	Search3Func     func(query string) ([]SubsonicSong, error)
	TriggerScanFunc func() (bool, error)
}

func (m *mockLibrary) Search3(query string) ([]SubsonicSong, error) {
	if m.Search3Func != nil {
		return m.Search3Func(query)
	}
	return nil, nil
}

func (m *mockLibrary) TriggerScan() (bool, error) {
	if m.TriggerScanFunc != nil {
		return m.TriggerScanFunc()
	}
	return false, nil
}

type mockYtdlp struct {
	DownloadAudioFunc   func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error)
	IsYtdlpAvailableFunc func() bool
}

func (m *mockYtdlp) DownloadAudio(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
	if m.DownloadAudioFunc != nil {
		return m.DownloadAudioFunc(ctx, rawURL, outputDir, audioFormat)
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

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	bitrate := 320
	p := &acquisitionPipeline{
		item: database.JobItem{JobID: 1, ID: 1},
		results: []SearchResult{
			{Filename: "track1.mp3", Score: 50.0, Size: 8_000_000, Bitrate: &bitrate},
			{Filename: "track2.flac", Score: 40.0, Size: 25_000_000},
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

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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
			{Filename: "track1.mp3", Score: 60.0, Size: 8_000_000, Bitrate: &bitrateMP3},
			{Filename: "track2.flac", Score: 50.0, Size: 25_000_000, Bitrate: &bitrateFLAC},
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

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	profile := &database.QualityProfile{
		AllowedFormats: "flac",
		PreferLossless: true,
	}

	bitrateMP3 := 320
	p := &acquisitionPipeline{
		item: database.JobItem{JobID: 1, ID: 1},
		profile: profile,
		results: []SearchResult{
			{Filename: "track1.mp3", Score: 50.0, Size: 8_000_000, Bitrate: &bitrateMP3}, // doesn't match profile
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
	// stageSelectBestResult guards against empty results instead of panicking:
	// the item is marked with a terminal no-results status and the stage skips.
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{
		item:    item,
		results: []SearchResult{},
	}

	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err, "guard must not error")
	require.True(t, skip, "expected skip=true on empty results")

	var updated database.JobItem
	require.NoError(t, db.First(&updated, item.ID).Error, "failed to fetch updated item")
	require.Equal(t, "failed (no results)", updated.Status)
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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	// Verify item got a terminal no-results status (not retryable 'failed').
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "failed (no results)" {
		t.Errorf("expected status 'failed (no results)', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageSearchSoulseek_Error(t *testing.T) {
	db := setupPipelineTestDB(t)

	slskdMock := &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			return nil, errors.New("network error")
		},
	}

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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
// stageCheckLibraryIndex tests
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageCheckLibraryIndex_LibraryMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	libraryMock := &mockLibrary{
		Search3Func: func(query string) ([]SubsonicSong, error) {
			return []SubsonicSong{
				{ID: "123", Title: "Test Track", Artist: "Test Artist"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, libraryMock, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckLibraryIndex(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (found in library)")
	}

	// Verify item was marked as completed
	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "completed (already indexed)" {
		t.Errorf("expected status 'completed (already indexed)', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageCheckLibraryIndex_CaseInsensitiveMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	libraryMock := &mockLibrary{
		Search3Func: func(query string) ([]SubsonicSong, error) {
			return []SubsonicSong{
				{ID: "456", Title: "test track", Artist: "test artist"},
			}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, libraryMock, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckLibraryIndex(p)
	require.NoError(t, err, "unexpected error")
	if !skip {
		t.Error("expected skip=true (case-insensitive match found in library)")
	}

	var updatedItem database.JobItem
	require.NoError(t, db.First(&updatedItem, item.ID).Error, "failed to fetch updated item")
	if updatedItem.Status != "completed (already indexed)" {
		t.Errorf("expected status 'completed (already indexed)', got %s", updatedItem.Status)
	}
}

func TestAcquisitionHandler_StageCheckLibraryIndex_NoLibraryConfigured(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckLibraryIndex(p)
	require.NoError(t, err, "unexpected error")
	if skip {
		t.Error("expected skip=false (no library configured, should continue)")
	}
}

func TestAcquisitionHandler_StageCheckLibraryIndex_NoMatch(t *testing.T) {
	db := setupPipelineTestDB(t)

	libraryMock := &mockLibrary{
		Search3Func: func(query string) ([]SubsonicSong, error) {
			return []SubsonicSong{}, nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, libraryMock, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "queued", NormalizedQuery: "test track", Artist: "Test Artist", TrackTitle: "Test Track", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}
	skip, err := handler.stageCheckLibraryIndex(p)
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
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			if rawURL == "https://youtube.com/watch?v=test" {
				return "/downloads/test.flac", nil
			}
			return "", errors.New("unexpected URL")
		},
	}

	cfg := &config.Config{DownloadStagingPath: "/downloads"}
	handler := NewAcquisitionHandler(db, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	downloaded, ok, _ := handler.stageYtdlpFallback(context.Background(), p)
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
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			t.Error("DownloadAudio should not be called when SourceURL is empty")
			return "", nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	downloaded, ok, _ := handler.stageYtdlpFallback(context.Background(), p)
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
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			t.Error("DownloadAudio should not be called when ytdlp unavailable")
			return "", nil
		},
	}

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok, _ := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (ytdlp unavailable)")
	}
}

func TestAcquisitionHandler_StageYtdlpFallback_DownloadError(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			return "", errors.New("download failed")
		},
	}

	cfg := &config.Config{DownloadStagingPath: "/downloads"}
	handler := NewAcquisitionHandler(db, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok, _ := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (download error)")
	}

	// An ordinary failure must be schedulable: the search stage left this item as
	// "failed (no results)", which has no next_attempt_at and is never re-claimed,
	// so logging the error alone would strand the item.
	var stored database.JobItem
	require.NoError(t, db.First(&stored, item.ID).Error)
	assert.Equal(t, "failed", stored.Status)
	require.NotNil(t, stored.NextAttemptAt, "an ordinary failure must be scheduled for retry")
}

func TestAcquisitionHandler_StageYtdlpFallback_YtdlpNil(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")
	item := database.JobItem{JobID: job.ID, Status: "failed", SourceURL: "https://youtube.com/watch?v=test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	p := &acquisitionPipeline{item: item}

	_, ok, _ := handler.stageYtdlpFallback(context.Background(), p)
	if ok {
		t.Error("expected ok=false (ytdlp nil)")
	}
}

// ---------------------------------------------------------------------------
// Execute trigger-scan tests
// Note: Execute tests are timing-sensitive due to the 5-second polling interval.
// We only test immediate-exit cases here (0 items, context cancellation).
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_NoResultsItem_Terminal(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running"}
	require.NoError(t, db.Create(&job).Error, "failed to create job")

	item := database.JobItem{JobID: job.ID, Status: "running", NormalizedQuery: "test", Sequence: 1}
	require.NoError(t, db.Create(&item).Error, "failed to create item")

	handler.noResultsItem(job.ID, item.ID, "No results found")

	var updated database.JobItem
	require.NoError(t, db.First(&updated, item.ID).Error, "failed to fetch item")
	require.Equal(t, "failed (no results)", updated.Status)
	require.Equal(t, "No results found", updated.FailureReason)
	require.NotNil(t, updated.FinishedAt, "terminal item must be finished")
	require.Zero(t, updated.RetryCount, "no-results must not consume retry attempts")
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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

	handler := NewAcquisitionHandler(db, nil, slskdMock, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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

// DownloadAudio verifies the output file exists before returning, so a fallback
// whose ownership write fails has left a real file on disk that no item points
// at. Importing it would be importing unowned bytes, and leaving it would make it
// the orphan scan's problem an hour later.
func TestAcquisitionHandler_StageYtdlpFallback_DiscardsAFileItCannotRecord(t *testing.T) {
	db := stagingImportTestDB(t)
	staging := t.TempDir()
	handler := NewAcquisitionHandler(db, &config.Config{DownloadStagingPath: staging},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, item := createAcquisitionTestItem(t, db)
	item.SourceURL = "https://example.test/watch?v=abc"
	// See the slskd case: a zero ID is what GORM refuses to update.
	item.ID = 0

	output := filepath.Join(staging, "Fallback Artist", "Fallback Album", "01 - Track.flac")
	require.NoError(t, os.MkdirAll(filepath.Dir(output), 0o755))
	require.NoError(t, os.WriteFile(output, []byte("fallback audio"), 0o644))

	handler.ytdlp = &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			return output, nil
		},
	}

	downloaded, ok, _ := handler.stageYtdlpFallback(context.Background(),
		&acquisitionPipeline{ctx: context.Background(), item: item})

	assert.False(t, ok, "a fallback whose path cannot be recorded must not continue")
	assert.Empty(t, downloaded)
	_, err := os.Stat(output)
	assert.True(t, os.IsNotExist(err), "the unattributable file must be discarded, not left in staging")
	_, err = os.Stat(filepath.Join(staging, "Fallback Artist"))
	assert.True(t, os.IsNotExist(err), "and the directories it emptied must be swept with it")
}

// A refused destination is a permanent verdict about the item's own source URL:
// the same URL resolves the same way, so the stage must surface it rather than
// swallow it into a retryable failure.
func TestAcquisitionHandler_StageYtdlpFallback_RefusedDestinationIsSurfaced(t *testing.T) {
	db := setupPipelineTestDB(t)

	ytdlpMock := &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			return "", fmt.Errorf("refusing source URL: %w: target internal.example resolves to private IP 10.0.0.5", ErrDisallowedDestination)
		},
	}

	cfg := &config.Config{DownloadStagingPath: t.TempDir()}
	handler := NewAcquisitionHandler(db, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, ytdlpMock)

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)
	item := database.JobItem{
		JobID: job.ID, Status: "failed", Sequence: 1,
		SourceURL: "http://internal.example/track.mp3",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	downloaded, ok, refusal := handler.stageYtdlpFallback(context.Background(), &acquisitionPipeline{item: item})

	assert.Empty(t, downloaded)
	assert.False(t, ok)
	require.ErrorIs(t, refusal, ErrDisallowedDestination,
		"the stage must surface a guard refusal for the caller to record terminally")
}

// And the caller records it through the existing terminal path, so the refusal is
// visible on the item and is not re-claimed for another attempt.
func TestAcquisitionHandler_ExecuteItem_RefusedDestinationAbandonsTheItem(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, &config.Config{
		DownloadStagingPath: t.TempDir(),
		MusicLibraryPath:    t.TempDir(),
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)

	item := database.JobItem{
		JobID: job.ID, Status: "queued", Sequence: 1,
		NormalizedQuery: "PUP PUP",
		Artist:          "PUP", Album: "PUP", TrackTitle: "PUP",
		SourceURL: "http://internal.example/track.mp3",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	// Soulseek finds nothing, which is exactly when the fallback runs.
	handler.slskd = &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			return nil, nil
		},
	}
	handler.ytdlp = &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			return "", fmt.Errorf("refusing source URL: %w: target internal.example resolves to private IP 10.0.0.5", ErrDisallowedDestination)
		},
	}

	require.NoError(t, handler.ExecuteItem(context.Background(), job.ID, item.ID))

	var stored database.JobItem
	require.NoError(t, db.First(&stored, item.ID).Error)
	assert.Equal(t, "abandoned", stored.Status,
		"a refused destination is terminal: a retry resolves the same URL the same way")
	assert.Nil(t, stored.NextAttemptAt,
		"a permanent verdict must not leave a retry scheduled")
	assert.NotNil(t, stored.FinishedAt)
	assert.Contains(t, stored.FailureReason, "disallowed",
		"the refusal must be visible on the item, not only in the log")

	nextID, claimErr := NewJobItemProcessor(db, handler).ClaimNextItem(job.ID)
	require.NoError(t, claimErr)
	assert.Zero(t, nextID, "an abandoned item is never re-claimed")
}

// abandonItem is a write, and it fails two ways: the item cannot be loaded, or the
// terminal state cannot be written. Both must reach the caller — the item keeps a
// claimable status, so reporting success would hide a verdict that was never
// recorded. This covers the load failure; the update failure is below.
func TestAcquisitionHandler_AbandonItem_ReportsLoadFailure(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)
	item := database.JobItem{JobID: job.ID, Status: "queued", Sequence: 1}
	require.NoError(t, db.Create(&item).Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = handler.abandonItem(job.ID, item.ID, "a verdict that cannot be written")

	require.Error(t, err, "an unrecorded abandonment must not look like success")
	assert.Contains(t, err.Error(), "abandon")
}

// The update path, which a closed database cannot reach: registering a GORM update
// callback fails the UPDATE while SELECTs keep working, which is exactly the
// condition the finding named. Without the error propagation this passes silently
// and the item is left running with a verdict nobody recorded.
func TestAcquisitionHandler_AbandonItem_ReportsUpdateFailure(t *testing.T) {
	db := setupPipelineTestDB(t)

	handler := NewAcquisitionHandler(db, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)
	item := database.JobItem{JobID: job.ID, Status: "running", Sequence: 1}
	require.NoError(t, db.Create(&item).Error)

	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_updates_for_test", func(tx *gorm.DB) {
		tx.AddError(errors.New("update refused"))
	}))

	err := handler.abandonItem(job.ID, item.ID, "a verdict that cannot be written")

	require.Error(t, err, "a write that failed must not be reported as a recorded verdict")
	assert.Contains(t, err.Error(), "record the abandonment")
}

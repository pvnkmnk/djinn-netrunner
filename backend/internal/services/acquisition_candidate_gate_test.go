package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Candidate plausibility gate
// ---------------------------------------------------------------------------

func TestCandidateRejectionReason(t *testing.T) {
	bitrate320 := 320
	bitrate1000 := 1000
	length240 := 240 // 4 minutes, in seconds

	tests := []struct {
		name     string
		result   SearchResult
		reject   bool
		contains string
	}{
		{
			name:   "normal mp3 accepted",
			result: SearchResult{Filename: `music\Artist\Album\01 - Track.mp3`, Size: 8_000_000, Bitrate: &bitrate320},
		},
		{
			name:   "lossless flac accepted",
			result: SearchResult{Filename: "music/Artist/Album/01 - Track.flac", Size: 30_000_000, Bitrate: &bitrate1000},
		},
		{
			// The file observed in the live library after an acquisition run.
			name:     "8 KB file named flac rejected",
			result:   SearchResult{Filename: `music\Converge\Jane Doe\01 - Concubine.flac`, Size: 8527},
			reject:   true,
			contains: "implausibly small flac",
		},
		{
			name:     "zero byte result rejected",
			result:   SearchResult{Filename: "track.mp3", Size: 0},
			reject:   true,
			contains: "implausibly small",
		},
		{
			name:     "non-audio extension rejected",
			result:   SearchResult{Filename: "Artist - Track.txt", Size: 8_000_000},
			reject:   true,
			contains: "unsupported audio format",
		},
		{
			name:     "missing filename rejected",
			result:   SearchResult{Filename: "   ", Size: 8_000_000},
			reject:   true,
			contains: "no filename",
		},
		{
			// A 4-minute 320kbps track cannot be 1 MB: the reported bitrate and
			// length let us catch a truncated file a flat floor would miss.
			name:     "undersized for reported bitrate and length rejected",
			result:   SearchResult{Filename: "track.mp3", Size: 1_000_000, Bitrate: &bitrate320, Length: &length240},
			reject:   true,
			contains: "implausibly small",
		},
		{
			name:   "size consistent with reported bitrate and length accepted",
			result: SearchResult{Filename: "track.mp3", Size: 9_600_000, Bitrate: &bitrate320, Length: &length240},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := candidateRejectionReason(tt.result)
			if tt.reject {
				assert.NotEmpty(t, reason, "expected rejection")
				assert.Contains(t, reason, tt.contains)
			} else {
				assert.Empty(t, reason, "expected acceptance, got %q", reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Selection filters while keeping the pick order intact
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageSelectBestResult_SkipsImplausibleResults(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	p := &acquisitionPipeline{
		item: item,
		results: []SearchResult{
			// Highest score, but not playable audio.
			{Filename: "Artist - Track.mp3", Size: 4096, Score: 99.0},
			{Filename: "Artist - Track.txt", Size: 9_000_000, Score: 90.0},
			{Filename: "Artist - Track.flac", Size: 30_000_000, Score: 50.0},
		},
	}

	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err)
	assert.False(t, skip)
	assert.Equal(t, "Artist - Track.flac", p.best.Filename,
		"the best usable candidate must win regardless of a junk result's score")
	require.Len(t, p.candidates, 1)

	// The rejection has to be visible on the job, or a sudden drop in usable
	// peers looks like an unexplained failure.
	assert.Contains(t, jobLogMessages(t, db, item.JobID), "Skipped 2 of 3 results")
}

func TestAcquisitionHandler_StageSelectBestResult_FailsWhenNothingIsUsable(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	p := &acquisitionPipeline{
		item: item,
		results: []SearchResult{
			{Filename: "Artist - Track.flac", Size: 8527, Score: 60.0},
			{Filename: "Artist - Track.mp3", Size: 0, Score: 50.0},
		},
	}

	skip, err := handler.stageSelectBestResult(p)
	require.NoError(t, err)
	assert.True(t, skip, "the item must not proceed to download without a usable candidate")

	var stored database.JobItem
	require.NoError(t, db.First(&stored, item.ID).Error)
	assert.Equal(t, "failed", stored.Status)
	assert.Contains(t, stored.FailureReason, "no usable candidate among 2 results")
	assert.Contains(t, stored.FailureReason, "implausibly small",
		"the reason must name why, so an operator is not left guessing")
}

// ---------------------------------------------------------------------------
// Download attempts move on when a peer cannot deliver
// ---------------------------------------------------------------------------

func TestAcquisitionHandler_StageDownloadFile_TriesNextCandidateWhenPeerNeverSends(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	var attempted []string
	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			attempted = append(attempted, username)
			return "id-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			if username == "dead-peer" {
				return nil, fmt.Errorf("%w: slskd state %q after 45s", ErrRemoteQueueStalled, "Queued, Remotely")
			}
			return &Download{ID: downloadID, Username: username, LocalPath: "/downloads/Album/01 - Track.flac"}, nil
		},
	}

	p := &acquisitionPipeline{
		item: item,
		candidates: []SearchResult{
			{Username: "dead-peer", Filename: "music/Album/01 - Track.flac", Size: 30_000_000},
			{Username: "good-peer", Filename: "music/Album/01 - Track.flac", Size: 30_000_000},
		},
		best: SearchResult{Username: "dead-peer", Filename: "music/Album/01 - Track.flac", Size: 30_000_000},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.False(t, skip, "a dead peer must not fail the item while another candidate could deliver it")
	assert.Equal(t, []string{"dead-peer", "good-peer"}, attempted)
	assert.Equal(t, "/downloads/Album/01 - Track.flac", p.download)

	logs := jobLogMessages(t, db, item.JobID)
	assert.Contains(t, logs, "never started sending — trying another candidate")
}

func TestAcquisitionHandler_StageDownloadFile_FailsAfterEveryCandidate(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			return nil, fmt.Errorf("%w: slskd state %q", ErrRemoteQueueStalled, "Queued, Remotely")
		},
	}

	p := &acquisitionPipeline{
		item: item,
		candidates: []SearchResult{
			{Username: "peer-a", Filename: "music/Album/01.flac", Size: 30_000_000},
			{Username: "peer-b", Filename: "music/Album/01.flac", Size: 30_000_000},
		},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.True(t, skip)

	var stored database.JobItem
	require.NoError(t, db.First(&stored, item.ID).Error)
	assert.Equal(t, "failed", stored.Status)
	assert.Contains(t, stored.FailureReason, "Download failed for all 2 candidate(s)")
	assert.Contains(t, stored.FailureReason, "never started sending")
}

func TestAcquisitionHandler_StageDownloadFile_BoundsCandidatesTried(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	attempts := 0
	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id", nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			attempts++
			return nil, fmt.Errorf("%w", ErrRemoteQueueStalled)
		},
	}

	p := &acquisitionPipeline{item: item}
	for i := 0; i < 10; i++ {
		p.candidates = append(p.candidates, SearchResult{
			Username: fmt.Sprintf("peer-%d", i),
			Filename: "music/Album/01.flac",
			Size:     30_000_000,
		})
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.True(t, skip)
	assert.Equal(t, downloadAttempts, attempts,
		"one item must not grind through every result it found")
}

// ---------------------------------------------------------------------------
// Downloaded bytes are validated before import
// ---------------------------------------------------------------------------

// Peers serve truncated rips and non-audio renamed to .mp3; the pre-download
// gate only sees advertised metadata, so the bytes themselves must be checked.
// A bad file must be rejected, deleted so it can never reach the library, and
// the next candidate given a chance.
func TestAcquisitionHandler_StageDownloadFile_RejectsUnplayableFileAndTriesNext(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor() // real ffprobe from PATH
	_, item := createAcquisitionTestItem(t, db)

	staging := t.TempDir()
	junk := filepath.Join(staging, "not-audio.mp3")
	require.NoError(t, os.WriteFile(junk, []byte("<html>404 Not Found</html>"), 0o644))
	real := filepath.Join(staging, "real.wav")
	writeSineWav(t, real)

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			local := junk
			if username == "good-peer" {
				local = real
			}
			return &Download{ID: downloadID, Username: username, LocalPath: local}, nil
		},
	}

	p := &acquisitionPipeline{
		ctx:  context.Background(),
		item: item,
		candidates: []SearchResult{
			{Username: "junk-peer", Filename: "music/Album/01.mp3", Size: 30_000_000},
			{Username: "good-peer", Filename: "music/Album/01.wav", Size: 30_000_000},
		},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.False(t, skip, "a peer serving junk must not fail the item while a good candidate remains")
	assert.Equal(t, real, p.download)

	_, statErr := os.Stat(junk)
	assert.True(t, os.IsNotExist(statErr),
		"the rejected file must be removed so it can never be imported later")

	logs := jobLogMessages(t, db, item.JobID)
	assert.Contains(t, logs, "delivered an unplayable file — rejected")
	assert.Contains(t, logs, "junk-peer")
}

// An unavailable ffprobe is a deployment gap, not a reason to fail every item:
// the file is imported and the gap is logged loudly.
func TestAcquisitionHandler_StageDownloadFile_ImportsWhenProbeUnavailable(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractorWithTools("", "definitely-not-a-real-ffprobe-binary")
	_, item := createAcquisitionTestItem(t, db)

	file := filepath.Join(t.TempDir(), "whatever.mp3")
	require.NoError(t, os.WriteFile(file, []byte("bytes"), 0o644))

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id", nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			return &Download{ID: downloadID, Username: username, LocalPath: file}, nil
		},
	}

	p := &acquisitionPipeline{
		ctx:        context.Background(),
		item:       item,
		candidates: []SearchResult{{Username: "peer", Filename: "music/Album/01.mp3", Size: 30_000_000}},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.False(t, skip, "an unavailable probe must not fail the item")
	assert.Equal(t, file, p.download)

	logs := jobLogMessages(t, db, item.JobID)
	assert.Contains(t, logs, "ffprobe", "the gap must be visible in the job log")
}

// A short, low-bitrate track is legitimate. The floor must come from the
// reported metadata rather than a fixed size, or a ten-second interlude is
// rejected before ffprobe ever sees the bytes.
func TestMinPlausibleSize_PrefersMetadataOverAFixedFloor(t *testing.T) {
	// 10s at 128 kbps implies ~160 KB, well under the 256 KiB floor this used to
	// apply unconditionally.
	short := minPlausibleSize(128, 10)
	assert.Greater(t, short, int64(0))
	assert.Less(t, short, int64(160_000), "a metadata-consistent short track must clear the floor")

	// Without metadata there is nothing to derive from, so a conservative
	// stand-in applies — still far above the few-kilobyte placeholders peers share.
	assert.Equal(t, int64(64<<10), minPlausibleSize(0, 0))
	assert.Greater(t, minPlausibleSize(0, 0), int64(8000))

	// A longer, higher-bitrate track implies a much larger floor.
	assert.Greater(t, minPlausibleSize(320, 240), int64(4_000_000))
}

func TestCandidateRejectionReason_AcceptsShortMetadataConsistentTrack(t *testing.T) {
	bitrate, length := 128, 10

	reason := candidateRejectionReason(SearchResult{
		Filename: "Artist/Album/01 - Interlude.mp3",
		Size:     150_000,
		Bitrate:  &bitrate,
		Length:   &length,
	})

	assert.Empty(t, reason, "a short track whose size matches its metadata is not implausible")
}

// Abandoning a peer must cancel the queued transfer. Left in slskd it can still
// start sending later, downloading a file nothing will import while consuming
// bandwidth and space in the shared staging volume.
func TestAcquisitionHandler_StageDownloadFile_CancelsAbandonedTransfers(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	var cancelled []string
	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "dl-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			return nil, fmt.Errorf("%w: slskd state %q", ErrRemoteQueueStalled, "Queued, Remotely")
		},
		CancelDownloadFunc: func(username, downloadID string) error {
			cancelled = append(cancelled, username+"/"+downloadID)
			return nil
		},
	}

	p := &acquisitionPipeline{
		ctx:  context.Background(),
		item: item,
		candidates: []SearchResult{
			{Username: "dead-a", Filename: "music/a.flac", Size: 30_000_000},
			{Username: "dead-b", Filename: "music/b.flac", Size: 30_000_000},
		},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.True(t, skip)
	assert.Equal(t, []string{"dead-a/dl-dead-a", "dead-b/dl-dead-b"}, cancelled,
		"every abandoned transfer must be cancelled, not just the first")
}

// Shutting the worker down must not delete a valid download. A cancelled probe is
// a statement about the job, not the file, so it unwinds the item instead of
// being reported as an unplayable file.
func TestAcquisitionHandler_StageDownloadFile_KeepsFileWhenProbeIsCancelled(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor()
	_, item := createAcquisitionTestItem(t, db)

	file := filepath.Join(t.TempDir(), "track.mp3")
	require.NoError(t, os.WriteFile(file, []byte("bytes"), 0o644))

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id", nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			return &Download{ID: downloadID, Username: username, LocalPath: file}, nil
		},
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	p := &acquisitionPipeline{
		ctx:        cancelled,
		item:       item,
		candidates: []SearchResult{{Username: "peer", Filename: "music/a.mp3", Size: 30_000_000}},
	}

	_, err := handler.stageDownloadFile(p)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "the item must unwind rather than be failed for good")

	_, statErr := os.Stat(file)
	assert.NoError(t, statErr, "a cancelled probe must not delete the download")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// createAcquisitionTestItem persists a running job and a queued item, which the
// failure paths load and update.
func createAcquisitionTestItem(t *testing.T, db *gorm.DB) (database.Job, database.JobItem) {
	t.Helper()

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)

	item := database.JobItem{
		JobID:           job.ID,
		Status:          "running",
		NormalizedQuery: "Artist Track",
		Artist:          "Artist",
		Sequence:        1,
	}
	require.NoError(t, db.Create(&item).Error)

	// executeItem-derived stages address the item by ID, so reload it the way
	// the pipeline does.
	require.NoError(t, db.First(&item, item.ID).Error)
	return job, item
}

// jobLogMessages returns every message logged against a job, joined for
// substring assertions.
func jobLogMessages(t *testing.T, db *gorm.DB, jobID uint64) string {
	t.Helper()

	var messages []string
	require.NoError(t, db.Table("job_logs").Where("job_id = ?", jobID).Pluck("message", &messages).Error)
	return fmt.Sprint(messages)
}

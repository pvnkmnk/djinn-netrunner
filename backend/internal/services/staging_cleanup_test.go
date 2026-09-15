package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discardStagedDownload is the single owner for "this staged file will never be
// imported". Before it existed, callers removed the file themselves (or not at
// all) and swept with cleanupEmptyStagingDirs, which only reclaims directories
// that are already empty — so a caller that left the file behind left the whole
// directory behind, and one discography run accumulated 13 of them.
func TestDiscardStagedDownload_RemovesFileAndSweepsEmptyDirs(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	file := filepath.Join(albumDir, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, 1, nil)

	_, err := os.Stat(file)
	assert.True(t, os.IsNotExist(err), "the staged file must be removed")
	_, err = os.Stat(albumDir)
	assert.True(t, os.IsNotExist(err), "the emptied album directory must be swept")
	_, err = os.Stat(filepath.Join(staging, "Some Artist"))
	assert.True(t, os.IsNotExist(err), "the emptied artist directory must be swept")
	_, err = os.Stat(staging)
	require.NoError(t, err, "the staging root itself must survive")
}

// A whole-album download is several files in one directory and each track is a
// separate item, so removing one duplicate must not take a sibling that another
// item still has to import.
func TestDiscardStagedDownload_KeepsSiblingsStillNeeded(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	dup := filepath.Join(albumDir, "01 - duplicate.m4a")
	other := filepath.Join(albumDir, "02 - still pending.flac")
	require.NoError(t, os.WriteFile(dup, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(other, []byte("y"), 0o644))

	h.discardStagedDownload(dup, 1, nil)

	_, err := os.Stat(dup)
	assert.True(t, os.IsNotExist(err), "the duplicate must be removed")
	_, err = os.Stat(other)
	require.NoError(t, err, "a sibling another item still needs must survive")
	_, err = os.Stat(albumDir)
	require.NoError(t, err, "a directory with remaining files must not be swept")
}

// Idempotent: resolving the same duplicate twice (a retry, or two items racing
// on one file) must not error and must still finish the sweep.
func TestDiscardStagedDownload_MissingFileStillSweeps(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	file := filepath.Join(albumDir, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))
	require.NoError(t, os.Remove(file))

	h.discardStagedDownload(file, 1, nil)
	h.discardStagedDownload(file, 1, nil) // second call must be a no-op

	_, err := os.Stat(albumDir)
	assert.True(t, os.IsNotExist(err), "an already-empty directory should still be reclaimed")
}

func TestDiscardStagedDownload_EmptyPathIsIgnored(t *testing.T) {
	h := &AcquisitionHandler{}
	require.NotPanics(t, func() { h.discardStagedDownload("", 1, nil) })
}

// Never climb out of the staging root, even when handed a path outside it.
func TestDiscardStagedDownload_LeavesDirsOutsideStagingRoot(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	outside := filepath.Join(t.TempDir(), "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	file := filepath.Join(outside, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, 1, nil)

	_, err := os.Stat(file)
	assert.True(t, os.IsNotExist(err), "the file is still removed")
	_, err = os.Stat(outside)
	require.NoError(t, err, "a directory outside the staging root must not be removed")
}

// Wiring: a peer that delivers an unplayable file has its bytes deleted, and the
// directory those bytes emptied must go too. The file removal predates this
// change; the sweep did not exist, which is why staging accumulated skeletons.
func TestAcquisitionHandler_RejectedDownloadSweepsEmptiedStagingDir(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	staging := t.TempDir()
	handler := NewAcquisitionHandler(db, &config.Config{DownloadStagingPath: staging},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor() // real ffprobe from PATH
	_, item := createAcquisitionTestItem(t, db)

	albumDir := filepath.Join(staging, "Junk Artist", "Junk Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	junk := filepath.Join(albumDir, "not-audio.mp3")
	require.NoError(t, os.WriteFile(junk, []byte("<html>404 Not Found</html>"), 0o644))

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			return &Download{ID: downloadID, Username: username, LocalPath: junk}, nil
		},
	}

	p := &acquisitionPipeline{
		ctx:        context.Background(),
		item:       item,
		candidates: []SearchResult{{Username: "junk-peer", Filename: "music/Album/01.mp3", Size: 30_000_000}},
	}

	// One candidate only, so the junk is fatal for the item — the point here is
	// what it leaves on disk, not whether another peer is tried.
	_, err := handler.stageDownloadFile(p)
	require.NoError(t, err)

	_, statErr := os.Stat(junk)
	require.True(t, os.IsNotExist(statErr), "the rejected file must be removed")

	_, statErr = os.Stat(albumDir)
	assert.True(t, os.IsNotExist(statErr), "the emptied album directory must be swept")
	_, statErr = os.Stat(filepath.Join(staging, "Junk Artist"))
	assert.True(t, os.IsNotExist(statErr), "the emptied artist directory must be swept")
	_, statErr = os.Stat(staging)
	require.NoError(t, statErr, "the staging root must survive")
}

// The containment check made the staging root absolute but left the incoming dir
// alone. filepath.Rel errors on that mixed pair, so the check bailed out and
// nothing was ever removed — invisible in Docker and in t.TempDir() tests, where
// the staging path is absolute, but the default "./downloads" is relative.
func TestCleanupEmptyStagingDirs_AcceptsRelativeDir(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))

	// Stand where a relative staging path resolves from, and hand the sweep the
	// relative form — exactly what filepath.Dir on "./downloads/..." produces.
	t.Chdir(staging)
	rel := filepath.Join("Some Artist", "Some Album")
	require.False(t, filepath.IsAbs(rel), "this test must exercise a relative dir")

	h.cleanupEmptyStagingDirs(rel, 1, nil)

	_, err := os.Stat(albumDir)
	assert.True(t, os.IsNotExist(err),
		"an empty staging dir given as a relative path must still be swept")
	_, err = os.Stat(filepath.Join(staging, "Some Artist"))
	assert.True(t, os.IsNotExist(err), "the emptied artist dir must be swept too")
	_, err = os.Stat(staging)
	require.NoError(t, err, "the staging root itself must survive")
}

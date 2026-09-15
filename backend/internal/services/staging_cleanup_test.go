package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

// A path outside the staging root is refused *whole*: the file is not removed
// either. Removing it and then failing the sweep would be a silent half-measure,
// and the contract this owner exists to keep is "only ever touch files the
// worker staged". The guard used to cover the directory sweep only, so the
// removal itself trusted its caller — and the number of callers grew.
func TestDiscardStagedDownload_RefusesPathOutsideStagingRoot(t *testing.T) {
	staging := t.TempDir()
	h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}

	outside := filepath.Join(t.TempDir(), "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	file := filepath.Join(outside, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, 1, nil)

	_, err := os.Stat(file)
	require.NoError(t, err, "a file outside the staging root must not be removed at all")
	_, err = os.Stat(outside)
	require.NoError(t, err, "a directory outside the staging root must not be removed")
	_, err = os.Stat(staging)
	require.NoError(t, err, "the staging root itself must survive")
}

// Containment is a path relationship, not a string prefix: a sibling whose name
// merely begins with the staging root's is still outside it.
func TestDiscardStagedDownload_RefusesSharedPrefixSibling(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "downloads")
	require.NoError(t, os.MkdirAll(staging, 0o755))
	h := &AcquisitionHandler{cfg: &config.Config{DownloadStagingPath: staging}}

	sibling := filepath.Join(t.TempDir(), "downloads-backup", "Some Album")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	file := filepath.Join(sibling, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, 1, nil)

	_, err := os.Stat(file)
	require.NoError(t, err, "a sibling sharing the staging root's prefix must not be touched")
}

// The guard has to resolve the default relative root exactly the way the removal
// does, or a stock deployment would refuse every legitimate discard. This is the
// mixed absolute/relative trap that once made the sweep silently never run.
func TestDiscardStagedDownload_DefaultRelativeRootStillDiscards(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	// Not t.TempDir(): the process cwd parks here during the test, which makes
	// Windows TempDir cleanup flaky. Best-effort manual cleanup instead.
	tmp, err := os.MkdirTemp("", "discard-default")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(wd)
		_ = os.RemoveAll(tmp)
	})
	require.NoError(t, os.Chdir(tmp))

	albumDir := filepath.Join(tmp, "downloads", "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	file := filepath.Join(albumDir, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	// An empty staging path is the default "./downloads" relative to the cwd.
	h := &AcquisitionHandler{cfg: &config.Config{}}
	h.discardStagedDownload(file, 1, nil)

	_, statErr := os.Stat(file)
	assert.True(t, os.IsNotExist(statErr),
		"a file under the default staging root must still be discarded")
	_, statErr = os.Stat(albumDir)
	assert.True(t, os.IsNotExist(statErr), "the emptied album directory must be swept")
	_, statErr = os.Stat(filepath.Join(tmp, "downloads"))
	require.NoError(t, statErr, "the staging root itself must survive")
}

// A refusal is only safe if it is visible. Declining to clean up silently is
// indistinguishable from "there was nothing to clean up", which would let a
// misconfigured download directory hide until the disk filled.
func TestDiscardStagedDownload_RefusalIsLogged(t *testing.T) {
	db := stagingImportTestDB(t)
	staging := t.TempDir()
	h := NewAcquisitionHandler(db, cfgWithStaging(t, staging),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, item := createAcquisitionTestItem(t, db)

	outside := filepath.Join(t.TempDir(), "Elsewhere")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	file := filepath.Join(outside, "01 - track.m4a")
	require.NoError(t, os.WriteFile(file, []byte("audio"), 0o644))

	h.discardStagedDownload(file, item.JobID, &item.ID)

	logs := jobLogMessages(t, db, item.JobID)
	assert.Contains(t, logs, "Refused to discard", "the refusal must reach the item's log")
	assert.Contains(t, logs, file, "the refusal must name what it refused")
}

// One owner resolves the staging root, so the download path resolver, the yt-dlp
// output directory and the sweep's containment check cannot disagree about where
// staging is. A disagreement is what makes an unguarded removal destructive.
func TestStagingRoot_IsTheSingleDefaultOwner(t *testing.T) {
	require.Equal(t, "./downloads", stagingRoot(nil))
	require.Equal(t, "./downloads", stagingRoot(&config.Config{}))
	require.Equal(t, "./downloads", stagingRoot(&config.Config{DownloadStagingPath: ""}))
	require.Equal(t, "/app/downloads", stagingRoot(&config.Config{DownloadStagingPath: "/app/downloads"}))
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

// stagingImportTestDB is file-backed rather than ":memory:". A memory DSN gives
// each *pooled connection* its own empty database, so the hash lookup these
// tests exercise could land on a connection that cannot see the seeded row —
// and the test would then pass without ever reaching the branch it is about.
func stagingImportTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "staging_import_test.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	// Windows will not delete the TempDir while the DB file is still open.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

func newStagingImportHandler(t *testing.T) (*AcquisitionHandler, *gorm.DB, string) {
	t.Helper()

	db := stagingImportTestDB(t)
	staging := t.TempDir()
	handler := NewAcquisitionHandler(db, &config.Config{DownloadStagingPath: staging},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor()

	return handler, db, staging
}

// stageStagedFile writes a download into staging and returns its path plus the
// album directory holding it, mirroring what a peer leaves behind.
func stageStagedFile(t *testing.T, staging string) (string, string) {
	t.Helper()

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))
	staged := filepath.Join(albumDir, "01 - track.mp3")
	require.NoError(t, os.WriteFile(staged, []byte("already in the library"), 0o644))

	return staged, albumDir
}

// assertStagingIsEmptyExceptRoot asserts the whole skeleton a download left
// behind is gone: the file, its album folder, and the artist folder.
func assertStagingIsEmptyExceptRoot(t *testing.T, staging, staged, albumDir, why string) {
	t.Helper()

	_, err := os.Stat(staged)
	assert.True(t, os.IsNotExist(err), "%s: the staged file must be removed", why)
	_, err = os.Stat(albumDir)
	assert.True(t, os.IsNotExist(err), "%s: the emptied album directory must be swept", why)
	_, err = os.Stat(filepath.Join(staging, "Some Artist"))
	assert.True(t, os.IsNotExist(err), "%s: the emptied artist directory must be swept", why)
	_, err = os.Stat(staging)
	require.NoError(t, err, "%s: the staging root itself must survive", why)
}

// A hash duplicate is the commonest duplicate there is — the same bytes
// downloaded twice. That branch marks the item completed and imports nothing, so
// the staged download is pure residue; before the import stage enforced this at
// its boundary, nothing removed it (the DJI-490 class, on the branch that never
// got the DJI-492 treatment).
func TestStageImportAndEnrich_HashDuplicateLeavesNoStagedDownload(t *testing.T) {
	handler, db, staging := newStagingImportHandler(t)
	_, item := createAcquisitionTestItem(t, db)
	staged, albumDir := stageStagedFile(t, staging)

	hash, err := handler.ext.HashFile(staged)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	require.NoError(t, db.Create(&database.Acquisition{
		JobID:        item.JobID,
		JobItemID:    item.ID,
		Artist:       "Some Artist",
		Album:        "Some Album",
		TrackTitle:   "Track",
		OriginalPath: staged,
		FinalPath:    filepath.Join(t.TempDir(), "Some Artist", "Some Album", "01 - track.mp3"),
		FileHash:     hash,
	}).Error)

	p := &acquisitionPipeline{ctx: context.Background(), item: item, download: staged}

	require.NoError(t, handler.stageImportAndEnrich(p.ctx, p))

	// The branch really was taken. Without this the test would also pass when
	// the file was imported instead — which removes it from staging for an
	// entirely different reason than the one under test.
	var got database.JobItem
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, "completed (duplicate hash)", got.Status)

	assertStagingIsEmptyExceptRoot(t, staging, staged, albumDir, "hash duplicate")
}

// A failed import is the other half of the same guarantee. importFile fails the
// item (scheduling a retry) and returns nil, leaving the download in staging —
// and the retry restarts the pipeline from the search stage, so it downloads
// again and never claims that file. The exit is terminal for the file even
// though the item will be tried again.
func TestStageImportAndEnrich_FailedImportLeavesNoStagedDownload(t *testing.T) {
	handler, db, staging := newStagingImportHandler(t)
	_, item := createAcquisitionTestItem(t, db)

	// The canonical-identity lookup reads `acquisitions`. Without the table it
	// reports a *failed query*, which must abort the import rather than be read
	// as "this artist is new" — the abort path is what leaves a staged file.
	require.NoError(t, db.Migrator().DropTable(&database.Acquisition{}))

	// The lookup only runs when the item carries an album, so give it one.
	require.NoError(t, db.Model(&database.JobItem{}).Where("id = ?", item.ID).
		Updates(map[string]interface{}{"album": "Some Album", "track_title": "Track"}).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	staged, albumDir := stageStagedFile(t, staging)
	p := &acquisitionPipeline{ctx: context.Background(), item: item, download: staged}

	require.NoError(t, handler.stageImportAndEnrich(p.ctx, p))

	// The item really did fail, so this is not the success path in disguise.
	var got database.JobItem
	require.NoError(t, db.First(&got, item.ID).Error)
	require.Equal(t, "failed", got.Status)

	assertStagingIsEmptyExceptRoot(t, staging, staged, albumDir, "failed import")
}

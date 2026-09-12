package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestScannerService(t *testing.T) {
	db := &gorm.DB{}
	s := NewScannerService(db)

	if s == nil {
		t.Fatal("Expected ScannerService to be initialized")
	}
}

func newScannerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// The scanner indexes with a goroutine pool, and a ":memory:" DSN gives each
	// pooled connection its own database — so the harness needs a file-backed DB
	// that every connection can open.
	dsn := filepath.Join(t.TempDir(), "scanner_test.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	// Track.Path is NOT NULL with a unique index, so the harness must create it
	// for the collision this test guards against to be possible at all.
	require.NoError(t, db.AutoMigrate(&database.Library{}, &database.Track{}))
	// Windows will not delete the TempDir while the DB file is still open, so
	// release the handle before TempDir's own cleanup runs.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// Regression: processFile created Track rows without ever setting Path. Because
// Path is NOT NULL with a unique index, the first file of a scan inserted fine
// and every later file failed on idx_tracks_path — so a library of N tracks
// silently indexed exactly one, with an empty path, and the job still reported
// "Completed".
func TestScanLibrary_IndexesEveryFileWithItsPath(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	db := newScannerTestDB(t)
	library := database.Library{Name: "Beta Library", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)

	albumDir := filepath.Join(library.Path, "Every Time I Die", "Gutter Phenomenon")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))

	// Three tracks in one album: more than one file is what triggers the
	// unique-index collision on an empty Path.
	onDisk := []string{
		generateTestAudio(t, albumDir, "01 - Kill the Music.flac",
			"-metadata", "title=Kill the Music", "-metadata", "artist=Every Time I Die",
			"-metadata", "album=Gutter Phenomenon"),
		generateTestAudio(t, albumDir, "02 - The New Black.flac",
			"-metadata", "title=The New Black", "-metadata", "artist=Every Time I Die",
			"-metadata", "album=Gutter Phenomenon"),
		generateTestAudio(t, albumDir, "03 - Imitation.flac",
			"-metadata", "title=Imitation", "-metadata", "artist=Every Time I Die",
			"-metadata", "album=Gutter Phenomenon"),
	}

	svc := NewScannerService(db)
	require.NoError(t, svc.ScanLibrary(context.Background(), library.ID, library.Path))

	var tracks []database.Track
	require.NoError(t, db.Order("path").Find(&tracks).Error)
	require.Len(t, tracks, len(onDisk), "every audio file must be indexed")

	seen := map[string]bool{}
	for _, tr := range tracks {
		require.NotEmpty(t, tr.Path, "path must be persisted — an empty path collides on idx_tracks_path")
		require.Equal(t, library.ID, tr.LibraryID)
		require.NotEmpty(t, tr.Title)
		require.Equal(t, "Every Time I Die", tr.Artist)
		require.Equal(t, "Gutter Phenomenon", tr.Album)
		require.FileExists(t, tr.Path)
		seen[tr.Path] = true
	}
	for _, want := range onDisk {
		require.True(t, seen[want], "expected %s to be indexed", want)
	}

	// A second scan is idempotent: it updates in place rather than duplicating.
	require.NoError(t, svc.ScanLibrary(context.Background(), library.ID, library.Path))
	var after int64
	require.NoError(t, db.Model(&database.Track{}).Count(&after).Error)
	require.EqualValues(t, len(onDisk), after, "re-scanning must not duplicate tracks")
}

// A scan that indexes some files and fails on others must surface an error
// instead of reporting blanket success — the worker marks the job failed on a
// non-nil return.
func TestScanLibrary_ReportsPartialFailure(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	db := newScannerTestDB(t)
	library := database.Library{Name: "Beta Library", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)

	generateTestAudio(t, library.Path, "01 - Good.flac", "-metadata", "title=Good")
	require.NoError(t, os.WriteFile(filepath.Join(library.Path, "02 - Corrupt.flac"),
		[]byte("not audio at all"), 0o644))

	svc := NewScannerService(db)
	err := svc.ScanLibrary(context.Background(), library.ID, library.Path)
	require.Error(t, err, "a file that cannot be indexed must not be silently skipped")

	var tracks []database.Track
	require.NoError(t, db.Find(&tracks).Error)
	require.Len(t, tracks, 1, "valid files must still be indexed")
}

// Cancelling after discovery but before the pool drains the queue left every
// worker returning early with err == nil and failed == 0, so the scan reported
// success while files it never looked at were silently dropped. The invariant
// is strict: returning nil means every file was indexed.
func TestScanLibrary_CancelledScanNeverReportsIncompleteSuccess(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	db := newScannerTestDB(t)
	library := database.Library{Name: "Beta Library", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)

	const total = 8
	for i := 0; i < total; i++ {
		generateTestAudio(t, library.Path, fmt.Sprintf("%02d - Track.flac", i),
			"-metadata", fmt.Sprintf("title=Track %d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel mid-flight so the race can land on either side of discovery; the
	// assertion below is what must hold regardless of which side it lands on.
	go func() {
		time.Sleep(time.Millisecond)
		cancel()
	}()
	defer cancel()

	err := NewScannerService(db).ScanLibrary(ctx, library.ID, library.Path)

	var indexed int64
	require.NoError(t, db.Model(&database.Track{}).Count(&indexed).Error)
	if err == nil {
		require.EqualValues(t, total, indexed,
			"a scan that reports success must have indexed every file")
	}
}

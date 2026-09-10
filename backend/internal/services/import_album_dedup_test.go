package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// cleanupEmptyStagingDirs must sweep empty dirs upward from the staged file's
// directory without crossing the staging root (observed: whole-album downloads
// leave empty album/artist skeletons in staging).
func TestCleanupEmptyStagingDirs(t *testing.T) {
	t.Run("removes empty album and artist dirs up to staging root", func(t *testing.T) {
		staging := t.TempDir()
		albumDir := filepath.Join(staging, "Some Artist", "Some Album")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))

		h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}
		h.cleanupEmptyStagingDirs(albumDir, 1, nil)

		// Both the album dir and the artist dir are gone; staging root remains.
		_, err := os.Stat(albumDir)
		require.True(t, os.IsNotExist(err), "album dir still exists after sweep")
		_, err = os.Stat(filepath.Join(staging, "Some Artist"))
		require.True(t, os.IsNotExist(err), "artist dir still exists after sweep")
		_, err = os.Stat(staging)
		require.NoError(t, err, "staging root itself was removed")
	})

	t.Run("stops at directory with remaining files", func(t *testing.T) {
		staging := t.TempDir()
		albumDir := filepath.Join(staging, "Some Artist", "Some Album")
		keep := filepath.Join(albumDir, "02 - kept.flac")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))
		require.NoError(t, os.WriteFile(keep, []byte("x"), 0o644))

		h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}
		h.cleanupEmptyStagingDirs(albumDir, 1, nil)

		_, err := os.Stat(albumDir)
		require.NoError(t, err, "album dir with remaining files was removed")
	})

	t.Run("never removes the staging root itself", func(t *testing.T) {
		staging := t.TempDir()
		h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}
		// Point the sweep directly at the staging root: even though it is
		// empty, the root must survive.
		h.cleanupEmptyStagingDirs(staging, 1, nil)

		_, err := os.Stat(staging)
		require.NoError(t, err, "staging root was removed")
	})

	t.Run("default staging root when cfg nil", func(t *testing.T) {
		wd, err := os.Getwd()
		require.NoError(t, err)
		// Not t.TempDir(): the process cwd parks inside this dir during the
		// test, which makes Windows TempDir cleanup flaky. Best-effort manual
		// cleanup instead — assertions below never depend on it.
		tmp, err := os.MkdirTemp("", "sweep-default")
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = os.Chdir(wd)
			_ = os.RemoveAll(tmp)
		})

		// Build ./downloads/<artist>/<album> inside a temp cwd so the default
		// staging root is deterministic and the sweep has real dirs to walk.
		require.NoError(t, os.Chdir(tmp))
		albumDir := filepath.Join(tmp, "downloads", "A", "B")
		require.NoError(t, os.MkdirAll(albumDir, 0o755))

		h := &AcquisitionHandler{}
		h.cleanupEmptyStagingDirs(albumDir, 1, nil)

		_, err = os.Stat(filepath.Join(tmp, "downloads"))
		require.NoError(t, err, "default staging root was removed")
		_, err = os.Stat(albumDir)
		require.True(t, os.IsNotExist(err), "album dir under default root still exists after sweep")
	})

	t.Run("dir outside staging root is left alone", func(t *testing.T) {
		staging := t.TempDir()
		elsewhere := filepath.Join(t.TempDir(), "empty-target")
		require.NoError(t, os.MkdirAll(elsewhere, 0o755))

		h := &AcquisitionHandler{}
		h.cleanupEmptyStagingDirs(elsewhere, 1, nil)

		_, err := os.Stat(elsewhere)
		require.NoError(t, err, "dir outside staging root was removed")
		_ = staging
	})

	t.Run("sibling directory with shared prefix is never touched", func(t *testing.T) {
		staging := t.TempDir()
		// "downloads2" shares the prefix "downloads" — a string-prefix guard
		// would consider it inside the staging root.
		sibling := filepath.Join(t.TempDir(), "downloads2", "A")
		require.NoError(t, os.MkdirAll(sibling, 0o755))

		h := &AcquisitionHandler{cfg: cfgWithStaging(t, staging)}
		h.cleanupEmptyStagingDirs(sibling, 1, nil)

		_, err := os.Stat(sibling)
		require.NoError(t, err, "sibling dir outside staging root was removed")
	})
}

func cfgWithStaging(t *testing.T, staging string) *config.Config {
	t.Helper()
	return &config.Config{DownloadStagingPath: staging}
}

func TestNormalizeAlbumTags_PanicGuarded(t *testing.T) {
	e := NewMetadataExtractor()

	// A truncated/garbage .m4a exercises the audiometa v3 MP4 parser without
	// needing a real audio file. Upstream v1.3.1 had a panic path in its covr
	// handling (unchecked covr.(*image.Image) assertion); v3 returns errors
	// instead — this test pins that the wrapper stays panic-free and that the
	// recover() backstop would convert any latent panic into a clean skip.
	path := filepath.Join(t.TempDir(), "track.m4a")
	require.NoError(t, os.WriteFile(path, []byte("garbage not an m4a"), 0o644))

	require.NotPanics(t, func() {
		_ = e.NormalizeAlbumTags(path, "Every Time I Die")
	})
}

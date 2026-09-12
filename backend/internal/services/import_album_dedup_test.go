package services

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
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

func TestNormalizeAlbumTags_GarbageFileSafety(t *testing.T) {
	e := NewMetadataExtractor()

	// A truncated/garbage .m4a must not crash the process and must surface a
	// clean error (ffmpeg cannot demux it). Import stays best-effort: the
	// caller logs and continues. The old audiometa path PANICKED here; ffmpeg
	// just fails the one file.
	path := filepath.Join(t.TempDir(), "track.m4a")
	require.NoError(t, os.WriteFile(path, []byte("garbage not an m4a"), 0o644))

	require.NotPanics(t, func() {
		err := e.NormalizeAlbumTags(context.Background(), path, "Every Time I Die")
		require.Error(t, err, "garbage input must produce an error, not a silent skip")
	})
	// The original file must survive the failed write.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "garbage not an m4a", string(data))
}

// ──────────────────────────────────────────────────────────────────────────
// FFmpegTagger round-trip tests (skipped when ffmpeg is unavailable, e.g.
// minimal CI runners; the Docker integration suite always has ffmpeg).
// ──────────────────────────────────────────────────────────────────────────

const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func generateTestAudio(t *testing.T, dir, name string, extraArgs ...string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
	args = append(args, extraArgs...)
	args = append(args, out)
	require.NoError(t, exec.Command("ffmpeg", args...).Run(), "ffmpeg audio generation failed")
	return out
}

// fileHash is a deterministic content identity for asserting whether a file
// was rewritten (mtime has too little resolution to be reliable).
func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return fmt.Sprintf("%x", md5.Sum(data))
}

func TestFFmpegTagger(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	tg := NewFFmpegTagger()
	ctx := context.Background()

	t.Run("stamps albumartist on m4a", func(t *testing.T) {
		path := generateTestAudio(t, t.TempDir(), "track.m4a")

		require.NoError(t, tg.StampAlbumArtist(ctx, path, "Every Time I Die"))

		m := readTagFile(path)
		require.NotNil(t, m, "rewritten file must still parse as audio")
		require.Equal(t, "Every Time I Die", m.AlbumArtist())

		// Idempotent: a file that already carries ALBUMARTIST is untouched
		// (no re-mux — content identity must not change).
		hashBefore := fileHash(t, path)
		require.NoError(t, tg.StampAlbumArtist(ctx, path, "Somebody Else"))
		require.Equal(t, hashBefore, fileHash(t, path), "existing tag must be a no-op")
	})

	t.Run("stamps albumartist on ogg", func(t *testing.T) {
		path := generateTestAudio(t, t.TempDir(), "track.ogg")
		require.NoError(t, tg.StampAlbumArtist(ctx, path, "Daryl Palumbo"))
		m := readTagFile(path)
		require.NotNil(t, m)
		require.Equal(t, "Daryl Palumbo", m.AlbumArtist())
	})

	t.Run("stamp preserves existing tags", func(t *testing.T) {
		path := generateTestAudio(t, t.TempDir(), "track.m4a",
			"-metadata", "title=Test Song", "-metadata", "artist=Orig Artist")
		require.NoError(t, tg.StampAlbumArtist(ctx, path, "Every Time I Die"))

		m := readTagFile(path)
		require.NotNil(t, m)
		require.Equal(t, "Every Time I Die", m.AlbumArtist())
		require.Equal(t, "Test Song", m.Title())
		require.Equal(t, "Orig Artist", m.Artist())

		// And the Extractor (dhowden/tag) still reads it all.
		e := NewMetadataExtractor()
		md, err := e.Extract(path)
		require.NoError(t, err)
		require.Equal(t, "Test Song", md.Title)
		require.Equal(t, "Orig Artist", md.Artist)
	})

	t.Run("embeds cover art on m4a once", func(t *testing.T) {
		png, err := base64.StdEncoding.DecodeString(testPNGBase64)
		require.NoError(t, err)
		path := generateTestAudio(t, t.TempDir(), "track.m4a")

		require.NoError(t, tg.EmbedCoverArt(ctx, path, png))
		m := readTagFile(path)
		require.NotNil(t, m)
		require.NotNil(t, m.Picture(), "cover art must be embedded")

		// Second embed is a no-op (first cover wins; no duplicate streams).
		hashBefore := fileHash(t, path)
		require.NoError(t, tg.EmbedCoverArt(ctx, path, png))
		require.Equal(t, hashBefore, fileHash(t, path))
	})

	t.Run("embeds cover art on ogg", func(t *testing.T) {
		png, err := base64.StdEncoding.DecodeString(testPNGBase64)
		require.NoError(t, err)
		path := generateTestAudio(t, t.TempDir(), "track.ogg")

		require.NoError(t, tg.EmbedCoverArt(ctx, path, png))
		m := readTagFile(path)
		require.NotNil(t, m)
		require.NotNil(t, m.Picture(), "cover art must be embedded via METADATA_BLOCK_PICTURE")

		// Second embed is a no-op.
		hashBefore := fileHash(t, path)
		require.NoError(t, tg.EmbedCoverArt(ctx, path, png))
		require.Equal(t, hashBefore, fileHash(t, path))
	})

	t.Run("garbage input errors without side effects", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "track.m4a")
		require.NoError(t, os.WriteFile(path, []byte("garbage not an m4a"), 0o644))

		require.Error(t, tg.StampAlbumArtist(ctx, path, "Every Time I Die"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "garbage not an m4a", string(data))
		leftovers, err := filepath.Glob(filepath.Join(dir, "*.tagtmp-*"))
		require.NoError(t, err)
		require.Empty(t, leftovers, "failed write must clean up its temp file")
	})

	t.Run("rejects unsupported extensions", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "track.wav")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
		require.Error(t, tg.StampAlbumArtist(ctx, path, "X"))
		require.Error(t, tg.EmbedCoverArt(ctx, path, []byte("x")))
	})
}

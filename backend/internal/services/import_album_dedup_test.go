package services

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
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
		err := e.NormalizeAlbumTags(context.Background(), path, AlbumTagIdentity{AlbumArtist: "Every Time I Die"})
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

// testCoverPNG returns deterministic, incompressible image data that clears
// MinimumCoverArtSize — Extractor.EmbedCoverArt rejects anything smaller, so a
// tiny fixture cannot exercise the embed path.
func testCoverPNG(t *testing.T) []byte {
	t.Helper()
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	seed := uint32(12345)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			seed = seed*1664525 + 1013904223
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(seed >> 24), G: uint8(seed >> 16), B: uint8(seed >> 8), A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.GreaterOrEqual(t, buf.Len(), MinimumCoverArtSize, "fixture must clear the minimum cover size")
	return buf.Bytes()
}

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

	t.Run("normalises identity on every imported format", func(t *testing.T) {
		// DJI-494: the tag is what a client groups by, so the canonical values
		// have to reach mp3 and flac too, not just the formats the audiometa
		// replacement happened to cover.
		for _, name := range []string{"track.mp3", "track.flac", "track.m4a", "track.ogg"} {
			path := generateTestAudio(t, t.TempDir(), name,
				"-metadata", "album_artist=Pup",
				"-metadata", "album=The Unraveling Of Puptheband",
				"-metadata", "artist=pup",
				"-metadata", "title=Lionheart")

			require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path, AlbumTagIdentity{
				AlbumArtist: "PUP",
				Album:       "The Unraveling of Puptheband",
				TrackArtist: "PUP",
			}), "%s: identity write failed", name)

			m := readTagFile(path)
			require.NotNil(t, m, "%s: rewritten file must still parse as audio", name)
			require.Equal(t, "PUP", m.AlbumArtist(), "%s: peer-cased album artist not corrected", name)
			require.Equal(t, "The Unraveling of Puptheband", m.Album(), "%s: album casing not corrected", name)
			require.Equal(t, "PUP", m.Artist(), "%s: peer-cased track artist not corrected", name)
			require.Equal(t, "Lionheart", m.Title(), "%s: unrelated tag lost in the rewrite", name)
		}
	})

	t.Run("corrects a disagreeing album artist instead of skipping it", func(t *testing.T) {
		// The live fragmentation: one album's tracks carry per-credit album
		// artists. The previous rule skipped any file that already had *a*
		// value, which is exactly why those files kept splitting the album.
		path := generateTestAudio(t, t.TempDir(), "track.m4a",
			"-metadata", "album_artist=Every Time I Die & Daryl Palumbo")

		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path,
			AlbumTagIdentity{AlbumArtist: "Every Time I Die"}))

		m := readTagFile(path)
		require.NotNil(t, m)
		require.Equal(t, "Every Time I Die", m.AlbumArtist())
	})

	t.Run("a genuine credit on the track artist survives", func(t *testing.T) {
		// The track artist is corrected only when it *is* the album artist with
		// different casing; overwriting a real performer credit would destroy
		// information the library holds nowhere else.
		path := generateTestAudio(t, t.TempDir(), "track.m4a",
			"-metadata", "album_artist=PUP", "-metadata", "artist=Daryl Palumbo")

		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path,
			AlbumTagIdentity{AlbumArtist: "PUP", TrackArtist: "PUP"}))

		m := readTagFile(path)
		require.NotNil(t, m)
		require.Equal(t, "Daryl Palumbo", m.Artist(), "a genuine credit must not be rewritten")
	})

	t.Run("an already-canonical file is not rewritten", func(t *testing.T) {
		// The no-op on agreement is what keeps a re-import from re-muxing the
		// whole library; only a disagreement is worth a write.
		path := generateTestAudio(t, t.TempDir(), "track.mp3",
			"-metadata", "album_artist=PUP", "-metadata", "album=PUP", "-metadata", "artist=PUP")
		want := AlbumTagIdentity{AlbumArtist: "PUP", Album: "PUP", TrackArtist: "PUP"}

		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path, want))
		hashBefore := fileHash(t, path)
		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path, want))
		require.Equal(t, hashBefore, fileHash(t, path), "a matching identity must not re-mux the file")

		cur := readTagFile(path)
		require.NotNil(t, cur)
		require.Empty(t, tagCorrectionArgs(cur, want, ".mp3"), "nothing disagrees, so nothing is written")
	})

	t.Run("identity write preserves embedded cover art", func(t *testing.T) {
		// The write re-muxes every stream, so it must not drop art the importer
		// embedded moments earlier.
		png := testCoverPNG(t)
		e := NewMetadataExtractor()
		for _, name := range []string{"art.mp3", "art.flac"} {
			path := generateTestAudio(t, t.TempDir(), name, "-metadata", "album_artist=Pup")
			require.NoError(t, e.EmbedCoverArt(ctx, path, png), "%s: fixture art embed failed", name)
			require.NotNil(t, readTagFile(path).Picture(), "%s: fixture must start with art", name)

			require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path,
				AlbumTagIdentity{AlbumArtist: "PUP"}))

			m := readTagFile(path)
			require.NotNil(t, m)
			require.Equal(t, "PUP", m.AlbumArtist(), "%s", name)
			require.NotNil(t, m.Picture(), "%s: identity write dropped the cover art", name)
		}
	})

	t.Run("a substantive album difference is left to the download gate", func(t *testing.T) {
		// The library can answer album casing from a folder, and folder names are
		// sanitised. Writing that back would replace the real title's punctuation.
		path := generateTestAudio(t, t.TempDir(), "track.mp3",
			"-metadata", "album=Triple J: Like a Version, Volume 13")

		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path, AlbumTagIdentity{
			Album: "Triple J- Like a Version, Volume 13",
		}))

		m := readTagFile(path)
		require.NotNil(t, m)
		require.Equal(t, "Triple J: Like a Version, Volume 13", m.Album(),
			"a sanitised folder name must not overwrite the real album title")
	})

	t.Run("extractor preserves unrelated tags through the write", func(t *testing.T) {
		path := generateTestAudio(t, t.TempDir(), "track.m4a",
			"-metadata", "title=Test Song", "-metadata", "artist=Orig Artist")
		require.NoError(t, tg.NormalizeAlbumIdentity(ctx, path,
			AlbumTagIdentity{AlbumArtist: "Every Time I Die"}))

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

	t.Run("the extractor skips formats whose tags it cannot write", func(t *testing.T) {
		// Best-effort contract: an unwritable container must not fail an import.
		dir := t.TempDir()
		path := generateTestAudio(t, dir, "track.wav")
		e := NewMetadataExtractor()
		require.NoError(t, e.NormalizeAlbumTags(ctx, path,
			AlbumTagIdentity{AlbumArtist: "PUP", Album: "PUP", TrackArtist: "PUP"}))
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

		require.Error(t, tg.NormalizeAlbumIdentity(ctx, path, AlbumTagIdentity{AlbumArtist: "Every Time I Die"}))

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
		require.Error(t, tg.NormalizeAlbumIdentity(ctx, path, AlbumTagIdentity{AlbumArtist: "X"}))
		require.Error(t, tg.EmbedCoverArt(ctx, path, []byte("x")))

		// Cover art for mp3/flac is embedded by the id3v2 and flac libraries in
		// the extractor, so widening the tag-write map must not silently widen
		// this attached-picture path.
		mp3 := generateTestAudio(t, t.TempDir(), "track.mp3")
		require.Error(t, tg.EmbedCoverArt(ctx, mp3, []byte("x")))
	})
}

// ---------------------------------------------------------------------------
// The import path itself, not just the tagger
// ---------------------------------------------------------------------------

// DJI-494's real shape: a peer delivers an mp3 whose tags are cased its own way
// (or carry a per-credit album artist), the file is filed under the canonical
// folder — and the tags, which are what a client groups by, stayed as the peer
// wrote them. This drives the import stage end to end and asserts the tags a
// client reads, not the folder.
func TestImportFile_CanonicalisesIdentityTagsOnMp3(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	staging := t.TempDir()
	libraryRoot := t.TempDir()

	h := NewAcquisitionHandler(db, &config.Config{
		DownloadStagingPath: staging,
		MusicLibraryPath:    libraryRoot,
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.ext = NewMetadataExtractor()

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)

	item := database.JobItem{
		JobID: job.ID, Status: "running", Sequence: 1,
		NormalizedQuery: "PUP PUP",
		Artist:          "PUP", Album: "PUP", TrackTitle: "Lionheart",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	// The peer's casing: every identity tag disagrees with the library's.
	download := generateTestAudio(t, staging, "01 - Lionheart.mp3",
		"-metadata", "album_artist=Pup",
		"-metadata", "album=pup",
		"-metadata", "artist=pup",
		"-metadata", "title=Lionheart")

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var imported []string
	require.NoError(t, filepath.WalkDir(libraryRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		imported = append(imported, path)
		return nil
	}))
	require.Len(t, imported, 1, "the file must be imported")
	require.Contains(t, filepath.ToSlash(imported[0]), "/PUP/PUP/",
		"the folder was already canonical before this change (DJI-489)")

	m := readTagFile(imported[0])
	require.NotNil(t, m, "imported file must still parse as audio")
	require.Equal(t, "PUP", m.AlbumArtist(), "a client groups by this tag, so the peer's casing must not survive import")
	require.Equal(t, "PUP", m.Album())
	require.Equal(t, "PUP", m.Artist())
	require.Equal(t, "Lionheart", m.Title(), "unrelated tags must survive the rewrite")
}

// The case the library actually shows: the album's tracks disagree per credit.
// The canonical folder alone did not fix it, because the tag is what the client
// reads.
func TestImportFile_CanonicalisesMultiCreditAlbumArtist(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	staging := t.TempDir()
	libraryRoot := t.TempDir()

	h := NewAcquisitionHandler(db, &config.Config{
		DownloadStagingPath: staging,
		MusicLibraryPath:    libraryRoot,
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.ext = NewMetadataExtractor()

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)

	item := database.JobItem{
		JobID: job.ID, Status: "running", Sequence: 1,
		NormalizedQuery: "Every Time I Die",
		Artist:          "Every Time I Die", Album: "Gutter Phenomenon", TrackTitle: "Kill the Music",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	download := generateTestAudio(t, staging, "01 - Kill the Music.flac",
		"-metadata", "album_artist=Every Time I Die & Daryl Palumbo",
		"-metadata", "artist=Every Time I Die & Daryl Palumbo",
		"-metadata", "album=Gutter Phenomenon",
		"-metadata", "title=Kill the Music")

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var imported []string
	require.NoError(t, filepath.WalkDir(libraryRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		imported = append(imported, path)
		return nil
	}))
	require.Len(t, imported, 1)

	m := readTagFile(imported[0])
	require.NotNil(t, m)
	require.Equal(t, "Every Time I Die", m.AlbumArtist(),
		"a per-credit album artist must be corrected, not preserved")

	// The track credit is a real collaboration, not a casing mistake, so it is
	// preserved: correcting it would destroy information the library holds
	// nowhere else.
	require.Equal(t, "Every Time I Die & Daryl Palumbo", m.Artist(),
		"a multi-credit track artist is preserved")
}

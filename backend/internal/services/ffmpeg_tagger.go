package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// FFmpegTagger writes container-level metadata (album artist, cover art) to
// M4A/OGG files by shelling out to ffmpeg. This replaces the audiometa
// library, whose MP4 parser panicked on real-world covr atoms; ffmpeg's
// demuxer/muxer handles the same files without crashing.
//
// Writes are lossless: `-c copy` reproduces every stream bit-for-bit and only
// metadata changes, so there is no re-encode and no quality loss. Every write
// goes to a unique sibling temp file and is swapped in atomically; the
// original file is never left in a half-written state.
type FFmpegTagger struct {
	// FFmpegPath is the ffmpeg binary; defaults to PATH lookup ("ffmpeg").
	FFmpegPath string
}

// NewFFmpegTagger returns a tagger using the ffmpeg binary from PATH.
func NewFFmpegTagger() *FFmpegTagger {
	return &FFmpegTagger{FFmpegPath: "ffmpeg"}
}

// NewFFmpegTaggerWithFFmpeg returns a tagger that uses the given ffmpeg
// binary (mirrors NewTranscoderServiceWithFFmpeg for tests).
func NewFFmpegTaggerWithFFmpeg(ffmpegPath string) *FFmpegTagger {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &FFmpegTagger{FFmpegPath: ffmpegPath}
}

// mapMuxer returns the ffmpeg output muxer for a file extension.
func mapMuxer(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".m4a", ".mp4", ".m4b":
		return "ipod", true // ipod muxer = strict MP4/M4A
	case ".ogg", ".opus", ".oga":
		return "ogg", true
	default:
		return "", false
	}
}

// readTagFile opens filePath with dhowden/tag for pure-Go tag reads.
// Returns nil when the file cannot be parsed; callers treat that as
// "tag not present" and let the ffmpeg write path surface real errors.
func readTagFile(filePath string) tag.Metadata {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil
	}
	return m
}

// runTagWrite re-muxes filePath (all streams copied losslessly) while
// applying extraOutputArgs (e.g. -metadata k=v), writing to a unique sibling
// temp file and atomically replacing the original. extraInputs, when given,
// are additional `-i` sources placed before the output options.
func (t *FFmpegTagger) runTagWrite(filePath string, extraInputs, extraOutputArgs []string) error {
	muxer, ok := mapMuxer(filepath.Ext(filePath))
	if !ok {
		return fmt.Errorf("unsupported extension for ffmpeg tagging: %s", filepath.Ext(filePath))
	}

	tmp, err := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".tagtmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for tagging: %w", err)
	}
	tmpPath := tmp.Name()
	// No-op after a successful rename; cleans up on any error exit.
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to create temp file for tagging: %w", err)
	}

	// Inputs first, then output options bound to the trailing output path.
	// Default metadata propagation copies the source tags through; extra
	// -metadata args add or override individual keys.
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", filePath}
	args = append(args, extraInputs...)
	args = append(args,
		"-map", "0",
		"-map_chapters", "0",
		"-c", "copy",
		"-f", muxer,
	)
	args = append(args, extraOutputArgs...)
	args = append(args, tmpPath)

	cmd := exec.Command(t.FFmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg tag write failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// os.CreateTemp uses 0600; keep the original file's permissions so media
	// servers running as another user can still read the imported file.
	if info, statErr := os.Stat(filePath); statErr == nil {
		if chmodErr := os.Chmod(tmpPath, info.Mode().Perm()); chmodErr != nil {
			return fmt.Errorf("failed to preserve file permissions: %w", chmodErr)
		}
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("failed to replace file with tagged copy: %w", err)
	}
	return nil
}

// StampAlbumArtist sets ALBUMARTIST on M4A/OGG files that do not already
// carry one. The pre-check reads the existing tag with dhowden/tag (pure Go,
// panic-free) and keeps the common already-stamped case a no-op, so the
// ffmpeg write only runs on files that actually need it.
func (t *FFmpegTagger) StampAlbumArtist(filePath, albumArtist string) error {
	if albumArtist == "" {
		return nil
	}
	if _, ok := mapMuxer(filepath.Ext(filePath)); !ok {
		return fmt.Errorf("unsupported extension for albumartist stamp: %s", filepath.Ext(filePath))
	}
	if m := readTagFile(filePath); m != nil && m.AlbumArtist() != "" {
		return nil
	}
	return t.runTagWrite(filePath, nil, []string{"-metadata", "album_artist=" + albumArtist})
}

// EmbedCoverArt attaches artData as front cover art to an M4A/OGG file.
// When the file already carries embedded artwork it is left untouched (no
// duplicate cover streams). The image is passed as a second input; marking
// it attached_pic makes ffmpeg store it as cover art (covr atom on MP4,
// METADATA_BLOCK_PICTURE on Ogg) while `-c copy` keeps the audio lossless.
func (t *FFmpegTagger) EmbedCoverArt(filePath string, artData []byte) error {
	if _, ok := mapMuxer(filepath.Ext(filePath)); !ok {
		return fmt.Errorf("unsupported extension for cover art embedding: %s", filepath.Ext(filePath))
	}
	if m := readTagFile(filePath); m != nil && m.Picture() != nil {
		return nil // already has artwork — first cover wins
	}

	tmpImg, err := os.CreateTemp("", filepath.Base(filePath)+".cover-*")
	if err != nil {
		return fmt.Errorf("failed to create temp image for embedding: %w", err)
	}
	tmpImgPath := tmpImg.Name()
	defer os.Remove(tmpImgPath)
	if _, err := tmpImg.Write(artData); err != nil {
		tmpImg.Close()
		return fmt.Errorf("failed to write temp image: %w", err)
	}
	if err := tmpImg.Close(); err != nil {
		return fmt.Errorf("failed to write temp image: %w", err)
	}

	return t.runTagWrite(filePath,
		[]string{"-i", tmpImgPath},
		[]string{"-map", "1", "-disposition:v", "attached_pic"},
	)
}

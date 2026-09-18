package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/go-flac/flacpicture/v2"
)

// tagWriteTimeout bounds a single ffmpeg tag write so a pathological file can
// never block a worker forever. Tag writes are lossless re-muxes of small
// audio files; two minutes is generous headroom.
const tagWriteTimeout = 2 * time.Minute

// FFmpegTagger writes container-level metadata (album artist, album, track
// artist, cover art) to mp3, flac, M4A and OGG files by shelling out to
// ffmpeg. This replaces the audiometa
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

// mapMuxer returns the ffmpeg output muxer for an extension whose tags the
// tagger can write with a lossless re-mux: the four containers the acquisition
// pipeline imports. mp3 and flac belong here because an identity tag the peer
// cased differently is what a client lists (DJI-494) — folder casing alone
// does not fix it.
func mapMuxer(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".m4a", ".mp4", ".m4b":
		return "ipod", true // ipod muxer = strict MP4/M4A
	case ".ogg", ".opus", ".oga":
		return "ogg", true
	case ".mp3":
		return "mp3", true
	case ".flac":
		return "flac", true
	default:
		return "", false
	}
}

// coverArtMuxer is the subset of mapMuxer whose cover art EmbedCoverArt can
// attach as a picture stream. mp3 and flac are deliberately absent: the
// extractor embeds their art with the id3v2 and flac libraries, so reaching
// here with those formats is a caller bug rather than art to stream-copy.
func coverArtMuxer(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".m4a", ".mp4", ".m4b", ".ogg", ".opus", ".oga":
		return mapMuxer(ext)
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
// The context bounds the ffmpeg process (plus the tagWriteTimeout backstop);
// deadline or cancellation kills the child process.
func (t *FFmpegTagger) runTagWrite(ctx context.Context, filePath string, extraInputs, extraOutputArgs []string) error {
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

	writeCtx, cancel := context.WithTimeout(ctx, tagWriteTimeout)
	defer cancel()
	cmd := exec.CommandContext(writeCtx, t.FFmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if writeCtx.Err() != nil {
			return fmt.Errorf("ffmpeg tag write timed out or was cancelled after %s", tagWriteTimeout)
		}
		return fmt.Errorf("ffmpeg tag write failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// os.CreateTemp uses 0600; keep the original file's permissions so media
	// servers running as another user can still read the imported file. A
	// missing source file is fatal — installing a 0600 replacement would be
	// worse than failing the (already doomed) write.
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("failed to stat source file before replace: %w", statErr)
	}
	if chmodErr := os.Chmod(tmpPath, info.Mode().Perm()); chmodErr != nil {
		return fmt.Errorf("failed to preserve file permissions: %w", chmodErr)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("failed to replace file with tagged copy: %w", err)
	}
	return nil
}

// oggCoverMetadata builds the base64 METADATA_BLOCK_PICTURE value for OGG
// cover art (the same FLAC picture block Ogg Vorbis comments carry). The
// picture block is built with flacpicture, which is already a dependency via
// the FLAC embed path. Cover bytes stay in-process; only the encoded block
// crosses the process boundary as one argv element (fine for cover-sized
// images on Linux, where ARG_MAX is ~2MB).
func oggCoverMetadata(artData []byte) ([]string, error) {
	pic, err := flacpicture.NewFromImageData(
		flacpicture.PictureTypeFrontCover,
		"Front Cover",
		artData,
		detectImageMimeType(artData),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build picture block: %w", err)
	}
	block := pic.Marshal()
	// Data holds the picture-block body; METADATA_BLOCK_PICTURE is the
	// base64 of exactly that (no FLAC block header byte).
	return []string{"-metadata", "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(block.Data)}, nil
}

// AlbumTagIdentity is the canonical identity to write into a file's tags. An
// empty field means "leave that tag alone".
type AlbumTagIdentity struct {
	// AlbumArtist is the artist the library files the album under. A value that
	// disagrees is corrected even when the file already carries an album artist
	// — a per-track credit such as "Every Time I Die & Daryl Palumbo" is the
	// fragmentation this exists to stop, and the peer's casing is the rest
	// (DJI-494).
	AlbumArtist string
	// Album is the canonical album name.
	Album string
	// TrackArtist is written only when the file's artist tag already matches it
	// case-insensitively, so correcting the peer's casing never erases a real
	// credit for another performer.
	TrackArtist string
}

// NormalizeAlbumIdentity makes a file's identity tags match want: a tag that
// already matches is left untouched (a re-import is not a rewrite), and a tag
// that disagrees is corrected rather than skipped — skipping the case-wrong
// value is exactly what left one artist listed twice (DJI-494).
func (t *FFmpegTagger) NormalizeAlbumIdentity(ctx context.Context, filePath string, want AlbumTagIdentity) error {
	if _, ok := mapMuxer(filepath.Ext(filePath)); !ok {
		return fmt.Errorf("unsupported extension for identity tag write: %s", filepath.Ext(filePath))
	}
	args := tagCorrectionArgs(readTagFile(filePath), want, filepath.Ext(filePath))
	if len(args) == 0 {
		return nil
	}
	return t.runTagWrite(ctx, filePath, nil, args)
}

// tagCorrectionArgs is the entire decision about what to write, kept pure so
// it is testable without ffmpeg: given the file's current tags (nil when the
// file cannot be parsed) and the canonical identity, it returns the
// `-metadata k=v` arguments for the fields that actually disagree. Unreadable
// tags mean there is nothing to compare against, so the canonical values are
// written — the same outcome as a file that carries no tags at all.
//
// The metadata level is per container, and getting it wrong is invisible:
// Ogg-family comments live on the stream, so a format-level `-metadata` write
// is accepted with no error and the old value stays in the file (verified
// against ffmpeg 9.0.1 — it only appeared to work when the file had no stream
// tags at all, which is why the previous album-artist stamp silently did
// nothing on a tagged Ogg). Every other supported container stores tags at the
// format level.
func tagCorrectionArgs(cur tag.Metadata, want AlbumTagIdentity, ext string) []string {
	key := "-metadata"
	switch strings.ToLower(ext) {
	case ".ogg", ".opus", ".oga":
		key = "-metadata:s:a:0"
	}

	var args []string
	if want.AlbumArtist != "" && (cur == nil || cur.AlbumArtist() != want.AlbumArtist) {
		args = append(args, key, "album_artist="+want.AlbumArtist)
	}
	// The album is corrected only when the difference is casing (or the tag
	// is empty). The library resolves an album's casing from the folder when
	// it has no history for it, and folder names are sanitised — writing
	// "Triple J- Like a Version, Volume 13" over a correct
	// "Triple J: Like a Version, Volume 13" would lose the real punctuation.
	// A substantive difference is a wrong-file signal for the download gate,
	// not something to paper over here.
	if want.Album != "" && (cur == nil || cur.Album() == "" ||
		(cur.Album() != want.Album && strings.EqualFold(cur.Album(), want.Album))) {
		args = append(args, key, "album="+want.Album)
	}
	// A case-only difference only: an artist tag that is not the album artist
	// is a credit for someone else, not a casing mistake.
	if want.TrackArtist != "" && cur != nil && cur.Artist() != want.TrackArtist &&
		strings.EqualFold(cur.Artist(), want.TrackArtist) {
		args = append(args, key, "artist="+want.TrackArtist)
	}
	return args
}

// EmbedCoverArt attaches artData as front cover art to an M4A/OGG file.
// When the file already carries embedded artwork it is left untouched (no
// duplicate cover streams). M4A takes the image as a second input stream
// marked attached_pic (covr atom); OGG takes a base64 METADATA_BLOCK_PICTURE
// Vorbis comment, because the OGG muxer cannot carry a picture stream.
func (t *FFmpegTagger) EmbedCoverArt(ctx context.Context, filePath string, artData []byte) error {
	ext := filepath.Ext(filePath)
	if _, ok := coverArtMuxer(ext); !ok {
		return fmt.Errorf("unsupported extension for cover art embedding: %s", ext)
	}
	if m := readTagFile(filePath); m != nil && m.Picture() != nil {
		return nil // already has artwork — first cover wins
	}

	switch strings.ToLower(ext) {
	case ".ogg", ".opus", ".oga":
		meta, err := oggCoverMetadata(artData)
		if err != nil {
			return err
		}
		return t.runTagWrite(ctx, filePath, nil, meta)
	default: // m4a/mp4/m4b
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
		return t.runTagWrite(ctx, filePath,
			[]string{"-i", tmpImgPath},
			[]string{"-map", "1", "-disposition:v", "attached_pic"},
		)
	}
}

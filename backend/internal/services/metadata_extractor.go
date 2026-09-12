package services

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
	"github.com/go-flac/flacpicture/v2"
	"github.com/go-flac/go-flac/v2"
)

type AudioMetadata struct {
	Artist      string
	AlbumArtist string
	Album       string
	Title       string
	TrackNumber int
	Year        int
	Format      string
	FileSize    int64
}

func (m *AudioMetadata) IsValid() bool {
	return m.Artist != "" && m.Title != ""
}

type MetadataExtractor struct {
	tagger *FFmpegTagger
}

func NewMetadataExtractor() *MetadataExtractor {
	return &MetadataExtractor{tagger: NewFFmpegTagger()}
}

// NewMetadataExtractorWithFFmpeg returns an extractor whose M4A/OGG tag
// writes shell out to the given ffmpeg binary (mirrors
// NewTranscoderServiceWithFFmpeg; used by tests).
func NewMetadataExtractorWithFFmpeg(ffmpegPath string) *MetadataExtractor {
	return &MetadataExtractor{tagger: NewFFmpegTaggerWithFFmpeg(ffmpegPath)}
}

// MinimumCoverArtSize is the minimum byte size for a valid cover art image (2KB).
const MinimumCoverArtSize = 2048

// detectImageMimeType detects the MIME type of image data from magic bytes.
func detectImageMimeType(data []byte) string {
	if len(data) < 4 {
		return "image/jpeg" // safe default
	}
	switch {
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return "image/png"
	case data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		return "image/gif"
	case len(data) >= 12 && string(data[8:12]) == "WEBP": // WEBP: RIFF....WEBP at offset 8
		return "image/webp"
	default:
		return "image/jpeg" // fallback for unknown formats
	}
}

// EmbedCoverArt embeds image data into the audio file
func (e *MetadataExtractor) EmbedCoverArt(ctx context.Context, filePath string, artData []byte) error {
	if len(artData) < MinimumCoverArtSize {
		return fmt.Errorf("cover art image too small (%d bytes), likely invalid", len(artData))
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3":
		return e.embedMP3(filePath, artData)
	case ".flac":
		return e.embedFLAC(filePath, artData)
	case ".m4a", ".ogg":
		return e.embedGeneric(ctx, filePath, artData)
	default:
		return fmt.Errorf("unsupported file format for cover art embedding: %s", ext)
	}
}

// embedGeneric embeds cover art into M4A/OGG files via FFmpegTagger
// (lossless re-mux; replaces the audiometa library that panicked on
// real-world MP4 covr atoms).
func (e *MetadataExtractor) embedGeneric(ctx context.Context, filePath string, artData []byte) error {
	return e.tagger.EmbedCoverArt(ctx, filePath, artData)
}

// NormalizeAlbumTags stamps a canonical ALBUMARTIST onto a downloaded file so
// media servers group a multi-credit album under one artist instead of
// fragmenting it per-track (observed live: "Every Time I Die & Daryl Palumbo"
// folders splitting one album three ways). The monitored artist is the
// canonical album artist because acquisition items are keyed on it. When the
// file already carries an ALBUMARTIST tag it is left untouched.
// Best-effort: tagging errors are logged, never fail the import.
func (e *MetadataExtractor) NormalizeAlbumTags(ctx context.Context, filePath, albumArtist string) error {
	if albumArtist == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".m4a", ".ogg":
		return e.tagger.StampAlbumArtist(ctx, filePath, albumArtist)
	default:
		// MP3/FLAC: the underlying libs here do not expose ALBUMARTIST
		// writing cleanly; the canonical folder layout below still groups
		// the album for media servers.
		slog.Debug("albumartist stamp not supported for format", "ext", ext)
		return nil
	}
}

func (e *MetadataExtractor) embedMP3(filePath string, artData []byte) error {
	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("failed to open mp3 for tagging: %w", err)
	}
	defer tag.Close()

	pic := id3v2.PictureFrame{
		Encoding:    id3v2.EncodingUTF8,
		MimeType:    detectImageMimeType(artData),
		PictureType: id3v2.PTFrontCover,
		Description: "Front Cover",
		Picture:     artData,
	}
	tag.AddAttachedPicture(pic)

	if err := tag.Save(); err != nil {
		return fmt.Errorf("failed to save mp3 tags: %w", err)
	}
	return nil
}

func (e *MetadataExtractor) embedFLAC(filePath string, artData []byte) error {
	f, err := flac.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse flac: %w", err)
	}

	pic, err := flacpicture.NewFromImageData(
		flacpicture.PictureTypeFrontCover,
		"Front Cover",
		artData,
		detectImageMimeType(artData),
	)
	if err != nil {
		return fmt.Errorf("failed to create flac picture block: %w", err)
	}

	block := pic.Marshal()
	f.Meta = append(f.Meta, &block)
	if err := f.Save(filePath); err != nil {
		return fmt.Errorf("failed to save flac: %w", err)
	}
	return nil
}

func (e *MetadataExtractor) Extract(path string) (*AudioMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read tags: %w", err)
	}

	metadata := &AudioMetadata{
		Artist:      m.Artist(),
		AlbumArtist: m.AlbumArtist(),
		Album:       m.Album(),
		Title:       m.Title(),
		Format:      string(m.FileType()),
	}

	track, _ := m.Track()
	metadata.TrackNumber = track
	metadata.Year = m.Year()

	info, err := os.Stat(path)
	if err == nil {
		metadata.FileSize = info.Size()
	}

	// Fallback for missing title
	if metadata.Title == "" {
		metadata.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return metadata, nil
}

func (e *MetadataExtractor) IsAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3", ".flac", ".m4a", ".ogg", ".opus", ".wav":
		return true
	}
	return false
}

func (e *MetadataExtractor) SanitizeFilename(text string) string {
	if text == "" {
		return "Unknown"
	}

	// Replace problematic characters
	replacements := map[string]string{
		"/":  "-",
		"\\": "-",
		":":  "-",
		"*":  "",
		"?":  "",
		"\"": "'",
		"<":  "",
		">":  "",
		"|":  "-",
	}

	for old, new := range replacements {
		text = strings.ReplaceAll(text, old, new)
	}

	return strings.TrimSpace(text)
}

// GenerateLibraryPath builds the library path for an imported file. When
// metadata carries an AlbumArtist (canonical grouping artist) it is used for
// the folder so multi-credit tracks ("X & Y", guest features) all land in one
// album folder instead of fragmenting the album per track credit.
func (e *MetadataExtractor) GenerateLibraryPath(metadata *AudioMetadata, libraryRoot string) string {
	artist := metadata.AlbumArtist
	if artist == "" {
		artist = metadata.Artist
	}
	artist = e.SanitizeFilename(artist)
	album := e.SanitizeFilename(metadata.Album)
	if album == "" {
		album = "Unknown Album"
	}
	title := e.SanitizeFilename(metadata.Title)

	var filename string
	if metadata.TrackNumber > 0 {
		filename = fmt.Sprintf("%02d - %s%s", metadata.TrackNumber, title, e.getExt(metadata.Format))
	} else {
		filename = fmt.Sprintf("%s%s", title, e.getExt(metadata.Format))
	}

	return filepath.Join(libraryRoot, artist, album, filename)
}

func (e *MetadataExtractor) HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (e *MetadataExtractor) Fingerprint(path string) (string, int, error) {
	// SECURITY: Validate path exists and is clean before passing to fpcalc
	cleanPath := filepath.Clean(path)
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		return "", 0, fmt.Errorf("file does not exist: %s", path)
	}

	cmd := exec.Command("fpcalc", "-json", "--", cleanPath)
	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("fpcalc failed: %w", err)
	}

	var result struct {
		Duration    int    `json:"duration"`
		Fingerprint string `json:"fingerprint"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse fpcalc output: %w", err)
	}

	return result.Fingerprint, result.Duration, nil
}

func (e *MetadataExtractor) getExt(format string) string {
	switch strings.ToUpper(format) {
	case "MP3":
		return ".mp3"
	case "FLAC":
		return ".flac"
	case "M4A", "AAC":
		return ".m4a"
	case "OGG":
		return ".ogg"
	case "OPUS":
		return ".opus"
	default:
		return ""
	}
}

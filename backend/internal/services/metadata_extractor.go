package services

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
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

type MetadataExtractor struct{}

func NewMetadataExtractor() *MetadataExtractor {
	return &MetadataExtractor{}
}

// EmbedCoverArt embeds image data into the audio file
func (e *MetadataExtractor) EmbedCoverArt(filePath string, artData []byte) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3":
		return e.embedMP3(filePath, artData)
	case ".flac":
		return e.embedFLAC(filePath, artData)
	default:
		return fmt.Errorf("unsupported file format for cover art embedding: %s", ext)
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
		MimeType:    "image/jpeg",
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
		"image/jpeg",
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
		Artist: m.Artist(),
		Album:  m.Album(),
		Title:  m.Title(),
		Format: string(m.FileType()),
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

func (e *MetadataExtractor) GenerateLibraryPath(metadata *AudioMetadata, libraryRoot string) string {
	artist := e.SanitizeFilename(metadata.Artist)
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
	cmd := exec.Command("fpcalc", "-json", path)
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

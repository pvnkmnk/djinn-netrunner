package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
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

type MetadataExtractor struct {}

func NewMetadataExtractor() *MetadataExtractor {
	return &MetadataExtractor{}
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

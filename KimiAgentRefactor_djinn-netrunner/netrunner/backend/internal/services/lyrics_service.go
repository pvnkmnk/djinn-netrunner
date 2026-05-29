package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// LyricsService handles lyrics fetching from LRCLIB
type LyricsService struct {
	BaseURL string
}

// NewLyricsService creates a new lyrics service
func NewLyricsService() *LyricsService {
	return &LyricsService{
		BaseURL: "https://lrclib.net/api",
	}
}

// Lyrics represents lyrics data from LRCLIB
type Lyrics struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// FetchLyrics fetches lyrics for a track from LRCLIB
func (s *LyricsService) FetchLyrics(ctx context.Context, artist, title, album string) (*Lyrics, error) {
	// Build search URL
	baseURL, err := url.Parse(s.BaseURL + "/search")
	if err != nil {
		return nil, err
	}

	// Add query parameters
	params := url.Values{}
	params.Set("artist_name", artist)
	params.Set("track_name", title)
	if album != "" {
		params.Set("album_name", album)
	}
	baseURL.RawQuery = params.Encode()

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "NetRunner/1.0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib api returned status: %d", resp.StatusCode)
	}

	// Parse response - LRCLIB returns an array of results
	var results []Lyrics
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no lyrics found for %s - %s", artist, title)
	}

	// Return the first result
	return &results[0], nil
}

// GetSyncedLyrics returns synced lyrics if available, otherwise plain lyrics
func (s *LyricsService) GetSyncedLyrics(lyrics *Lyrics) string {
	if lyrics == nil {
		return ""
	}

	// Prefer synced lyrics if available
	if lyrics.SyncedLyrics != "" {
		return lyrics.SyncedLyrics
	}

	// Fall back to plain lyrics
	return lyrics.PlainLyrics
}

// IsInstrumental checks if the track is instrumental
func (s *LyricsService) IsInstrumental(lyrics *Lyrics) bool {
	if lyrics == nil {
		return false
	}
	return lyrics.Instrumental
}

// FormatAsLRC formats lyrics as LRC file content
func (s *LyricsService) FormatAsLRC(lyrics *Lyrics) string {
	if lyrics == nil {
		return ""
	}

	// If we have synced lyrics, return them as-is
	if lyrics.SyncedLyrics != "" {
		return lyrics.SyncedLyrics
	}

	// For plain lyrics, we can't create proper LRC without timing
	// Return empty string to indicate no synced lyrics available
	return ""
}

// FormatAsText formats lyrics as plain text
func (s *LyricsService) FormatAsText(lyrics *Lyrics) string {
	if lyrics == nil {
		return ""
	}

	// Return plain lyrics
	return lyrics.PlainLyrics
}

// CleanLyrics removes extra whitespace and normalizes line endings
func (s *LyricsService) CleanLyrics(lyrics string) string {
	if lyrics == "" {
		return ""
	}

	// Normalize line endings
	lyrics = strings.ReplaceAll(lyrics, "\r\n", "\n")
	lyrics = strings.ReplaceAll(lyrics, "\r", "\n")

	// Remove trailing whitespace from each line
	lines := strings.Split(lyrics, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	return strings.Join(cleanedLines, "\n")
}

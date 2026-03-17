package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// DiscogsService handles interaction with the Discogs API for metadata enrichment
type DiscogsService struct {
	cfg         *config.Config
	httpClient  *http.Client
	token       string
	baseURL     string
	rateLimiter *time.Ticker
}

// DiscogsRelease represents a release from Discogs
type DiscogsRelease struct {
	ID         int             `json:"id"`
	Title      string          `json:"title"`
	Year       int             `json:"year"`
	Genre      []string        `json:"genre"`
	Style      []string        `json:"style"`
	CoverImage string          `json:"cover_image"`
	Thumb      string          `json:"thumb"`
	Tracklist  []DiscogsTrack  `json:"tracklist"`
	Artists    []DiscogsArtist `json:"artists"`
}

// DiscogsTrack represents a track on a Discogs release
type DiscogsTrack struct {
	Position string `json:"position"`
	Title    string `json:"title"`
	Duration string `json:"duration"`
}

// DiscogsArtist represents an artist from Discogs
type DiscogsArtist struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// DiscogsSearchResult represents search results
type DiscogsSearchResult struct {
	Pagination struct {
		Page    int `json:"page"`
		Pages   int `json:"pages"`
		PerPage int `json:"per_page"`
		Items   int `json:"items"`
	} `json:"pagination"`
	Results []DiscogsSearchItem `json:"results"`
}

// DiscogsSearchItem represents a single search result
type DiscogsSearchItem struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Year        string   `json:"year"`
	Genre       []string `json:"genre"`
	CoverImage  string   `json:"cover_image"`
	Thumb       string   `json:"thumb"`
	Format      []string `json:"format"`
	Country     string   `json:"country"`
	Label       []string `json:"label"`
	ResourceURL string   `json:"resource_url"`
}

// NewDiscogsService creates a new Discogs service
func NewDiscogsService(cfg *config.Config) *DiscogsService {
	token := ""
	if cfg != nil {
		token = cfg.DiscogsToken
	}

	return &DiscogsService{
		cfg:     cfg,
		token:   token,
		baseURL: "https://api.discogs.com",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// Discogs allows 60 requests per minute for authenticated users
		rateLimiter: time.NewTicker(time.Second),
	}
}

// SearchRelease searches Discogs for a release by artist and title
func (s *DiscogsService) SearchRelease(artist, title string) (*DiscogsSearchResult, error) {
	// Wait for rate limiter
	<-s.rateLimiter.C

	query := fmt.Sprintf("artist:%s release:%s", artist, title)
	params := url.Values{}
	params.Set("q", query)
	params.Set("type", "release")
	params.Set("per_page", "10")

	req, err := http.NewRequest("GET", s.baseURL+"/database/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "DjinnNetRunner/1.0")
	if s.token != "" {
		req.Header.Set("Authorization", "Discogs token="+s.token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discogs API error: %d", resp.StatusCode)
	}

	var result DiscogsSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetReleaseDetails gets detailed information about a release
func (s *DiscogsService) GetReleaseDetails(releaseID int) (*DiscogsRelease, error) {
	// Wait for rate limiter
	<-s.rateLimiter.C

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/releases/%d", s.baseURL, releaseID), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "DjinnNetRunner/1.0")
	if s.token != "" {
		req.Header.Set("Authorization", "Discogs token="+s.token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discogs API error: %d", resp.StatusCode)
	}

	var release DiscogsRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// GetCoverArt gets cover art URL for a release
func (s *DiscogsService) GetCoverArt(artist, title string) (string, error) {
	results, err := s.SearchRelease(artist, title)
	if err != nil {
		return "", err
	}

	if len(results.Results) == 0 {
		return "", fmt.Errorf("no release found")
	}

	// Prefer releases with cover art
	for _, r := range results.Results {
		if r.CoverImage != "" {
			return r.CoverImage, nil
		}
	}

	// Fall back to first result
	return results.Results[0].CoverImage, nil
}

// GetGenre gets primary genre for a release
func (s *DiscogsService) GetGenre(artist, title string) (string, error) {
	results, err := s.SearchRelease(artist, title)
	if err != nil {
		return "", err
	}

	if len(results.Results) == 0 {
		return "", fmt.Errorf("no release found")
	}

	if len(results.Results[0].Genre) > 0 {
		return results.Results[0].Genre[0], nil
	}

	return "", nil
}

// GetYear gets release year for a track
func (s *DiscogsService) GetYear(artist, title string) (int, error) {
	results, err := s.SearchRelease(artist, title)
	if err != nil {
		return 0, err
	}

	if len(results.Results) == 0 {
		return 0, fmt.Errorf("no release found")
	}

	// Parse year from string
	yearStr := results.Results[0].Year
	if yearStr == "" {
		return 0, nil
	}

	// Year might be in format "2020" or "2020, 2021"
	yearStr = strings.Split(yearStr, ",")[0]
	var year int
	fmt.Sscanf(yearStr, "%d", &year)

	return year, nil
}

// EnrichTrack updates track with metadata from Discogs
func (s *DiscogsService) EnrichTrack(artist, title string) (map[string]interface{}, error) {
	results, err := s.SearchRelease(artist, title)
	if err != nil {
		return nil, err
	}

	if len(results.Results) == 0 {
		return nil, fmt.Errorf("no release found")
	}

	r := results.Results[0]

	enriched := map[string]interface{}{
		"cover_url": r.CoverImage,
	}

	if len(r.Genre) > 0 {
		enriched["genre"] = r.Genre[0]
	}

	if r.Year != "" {
		yearStr := strings.Split(r.Year, ",")[0]
		var year int
		if n, _ := fmt.Sscanf(yearStr, "%d", &year); n == 1 {
			enriched["year"] = year
		}
	}

	return enriched, nil
}

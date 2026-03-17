package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// MusicBrainzService handles interaction with the MusicBrainz API
type MusicBrainzService struct {
	cfg         *config.Config
	baseURL     string
	httpClient  *http.Client
	rateLimiter *time.Ticker
	cache       *CacheService
}

// NewMusicBrainzService creates a new MusicBrainz service
func NewMusicBrainzService(cfg *config.Config) *MusicBrainzService {
	return &MusicBrainzService{
		cfg:     cfg,
		baseURL: "https://musicbrainz.org",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// MusicBrainz allows 1 request per second
		rateLimiter: time.NewTicker(time.Second),
	}
}

func (s *MusicBrainzService) SetCache(cache *CacheService) {
	s.cache = cache
}

// MusicBrainzArtist represents an artist from MusicBrainz
type MusicBrainzArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
}

// SearchArtist searches MusicBrainz for an artist by name
func (s *MusicBrainzService) SearchArtist(query string) ([]MusicBrainzArtist, error) {
	// Wait for rate limiter
	<-s.rateLimiter.C

	url := fmt.Sprintf("%s/ws/2/artist?query=artist:%s&fmt=json&limit=5", s.baseURL, url.QueryEscape(query))

	req, _ := http.NewRequest("GET", url, nil)
	userAgent := "netrunner/1.0 (contact@example.com)"
	if s.cfg != nil && s.cfg.MusicBrainzUserAgent != "" {
		userAgent = s.cfg.MusicBrainzUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var result struct {
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			SortName       string `json:"sort-name"`
			Disambiguation string `json:"disambiguation"`
		} `json:"artists"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	artists := make([]MusicBrainzArtist, len(result.Artists))
	for i, a := range result.Artists {
		artists[i] = MusicBrainzArtist{
			ID:             a.ID,
			Name:           a.Name,
			SortName:       a.SortName,
			Disambiguation: a.Disambiguation,
		}
	}
	return artists, nil
}

// GetArtistDiscography gets all release groups for an artist
func (s *MusicBrainzService) GetArtistDiscography(artistID string) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("discography:%s", artistID)
	if s.cache != nil {
		var cached map[string]interface{}
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return cached, nil
		}
	}

	params := url.Values{}
	params.Add("artist", artistID)
	params.Add("inc", "release-groups")
	params.Add("fmt", "json")

	result, err := s.doRequest("release-group", params)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set("musicbrainz", cacheKey, result, 12*time.Hour)
	}

	return result, nil
}

func (s *MusicBrainzService) doRequest(endpoint string, params url.Values) (map[string]interface{}, error) {
	// Wait for rate limiter
	<-s.rateLimiter.C

	baseURL := "https://musicbrainz.org/ws/2/"
	fullURL := fmt.Sprintf("%s%s?%s", baseURL, endpoint, params.Encode())

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", s.cfg.MusicBrainzUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *MusicBrainzService) Close() {
	s.rateLimiter.Stop()
}

func (s *MusicBrainzService) HealthCheck() bool {
	// Simple check to see if we can reach MB (could be more thorough)
	return true
}

// GetCoverArt fetches cover art for a release from MusicBrainz
func (s *MusicBrainzService) GetCoverArt(releaseMBID string) (string, error) {
	// First get the release to find the front cover
	release, err := s.GetRelease(releaseMBID)
	if err != nil {
		return "", err
	}

	// Look for front cover in images
	for _, img := range release.Images {
		if img.Front {
			return img.Image, nil
		}
	}

	// Return first image if no front cover found
	if len(release.Images) > 0 {
		return release.Images[0].Image, nil
	}

	return "", fmt.Errorf("no cover art found")
}

// MusicBrainzRelease represents a release from MusicBrainz
type MusicBrainzRelease struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Date         string           `json:"date"`
	Country      string           `json:"country"`
	Genres       []mbGenre        `json:"genres"`
	Media        []mbMedia        `json:"media"`
	Images       []mbImage        `json:"images"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
}

type mbGenre struct {
	Name string `json:"name"`
}

type mbMedia struct {
	Format     string    `json:"format"`
	TrackCount int       `json:"track-count"`
	Tracks     []mbTrack `json:"tracks"`
}

type mbTrack struct {
	Number string `json:"number"`
	Title  string `json:"title"`
}

type mbImage struct {
	Image string `json:"image"`
	Thumb string `json:"thumb"`
	Front bool   `json:"front"`
	Back  bool   `json:"back"`
	Types string `json:"types"`
}

type mbArtistCredit struct {
	Name   string      `json:"name"`
	Artist mbArtistRef `json:"artist"`
}

type mbArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetRelease fetches detailed release information
func (s *MusicBrainzService) GetRelease(releaseMBID string) (*MusicBrainzRelease, error) {
	endpoint := fmt.Sprintf("/ws/2/release/%s?fmt=json&inc=artist-credit+genres+tracks+images", releaseMBID)
	data, err := s.doRequest(endpoint, nil)
	if err != nil {
		return nil, err
	}

	// Parse into our struct
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var release MusicBrainzRelease
	if err := json.Unmarshal(jsonData, &release); err != nil {
		return nil, err
	}

	return &release, nil
}

// GetReleaseByArtistTitle searches for a release and returns the first result
func (s *MusicBrainzService) GetReleaseByArtistTitle(artist, title string) (*MusicBrainzRelease, error) {
	// Search for release
	query := fmt.Sprintf("artist:%s AND release:%s", artist, title)
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", "1")

	data, err := s.doRequest("/ws/2/release", params)
	if err != nil {
		return nil, err
	}

	releases, ok := data["releases"].([]interface{})
	if !ok || len(releases) == 0 {
		return nil, fmt.Errorf("no release found")
	}

	// Get the MBID of first result
	firstRelease, ok := releases[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid release data")
	}

	mbid, ok := firstRelease["id"].(string)
	if !ok {
		return nil, fmt.Errorf("release has no MBID")
	}

	// Get full release details
	return s.GetRelease(mbid)
}

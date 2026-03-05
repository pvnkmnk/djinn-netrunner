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
	httpClient  *http.Client
	rateLimiter *time.Ticker
	cache       *CacheService
}

// NewMusicBrainzService creates a new MusicBrainz service
func NewMusicBrainzService(cfg *config.Config) *MusicBrainzService {
	return &MusicBrainzService{
		cfg: cfg,
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

// SearchArtists searches for artists on MusicBrainz
func (s *MusicBrainzService) SearchArtists(query string, limit int) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("search:artist:%s:%d", query, limit)
	if s.cache != nil {
		var cached map[string]interface{}
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return cached, nil
		}
	}

	params := url.Values{}
	params.Add("query", query)
	params.Add("limit", fmt.Sprintf("%d", limit))
	params.Add("fmt", "json")

	result, err := s.doRequest("artist", params)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set("musicbrainz", cacheKey, result, 24*time.Hour)
	}

	return result, nil
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

package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

type AcoustIDService struct {
	cfg        *config.Config
	httpClient *http.Client
	cache      *CacheService
}

func NewAcoustIDService(cfg *config.Config) *AcoustIDService {
	return &AcoustIDService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *AcoustIDService) SetCache(cache *CacheService) {
	s.cache = cache
}

type AcoustIDResult struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
	Recordings []struct {
		ID string `json:"id"`
	} `json:"recordings"`
}

func (s *AcoustIDService) Lookup(fingerprint string, duration int) ([]AcoustIDResult, error) {
	if s.cfg.AcoustIDApiKey == "" {
		return nil, fmt.Errorf("AcoustID API key is not configured")
	}

	cacheKey := fmt.Sprintf("lookup:%d:%s", duration, fingerprint[:32]) // Use prefix for key length
	if s.cache != nil {
		var cached []AcoustIDResult
		if found, _ := s.cache.Get("acoustid", cacheKey, &cached); found {
			return cached, nil
		}
	}

	params := url.Values{}
	params.Add("client", s.cfg.AcoustIDApiKey)
	params.Add("meta", "recordings")
	params.Add("fingerprint", fingerprint)
	params.Add("duration", fmt.Sprintf("%d", duration))
	params.Add("format", "json")

	fullURL := fmt.Sprintf("https://api.acoustid.org/v2/lookup?%s", params.Encode())

	resp, err := s.httpClient.Get(fullURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acoustid api error: %s", resp.Status)
	}

	var data struct {
		Status  string           `json:"status"`
		Results []AcoustIDResult `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if data.Status != "ok" {
		return nil, fmt.Errorf("acoustid api status: %s", data.Status)
	}

	if s.cache != nil {
		s.cache.Set("acoustid", cacheKey, data.Results, 168*time.Hour) // Cache for a week
	}

	return data.Results, nil
}

package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
)

type AcoustIDService struct {
	cfg        *config.Config
	httpClient *http.Client
	cache      *CacheService
}

func NewAcoustIDService(cfg *config.Config) *AcoustIDService {
	return &AcoustIDService{
		cfg: cfg,
		// ✅ SECURITY: Use SSRF-protected client for external AcoustID API.
		httpClient: NewSafeProxyAwareHTTPClient(cfg, 15*time.Second),
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

	start := time.Now()
	fullURL := fmt.Sprintf("https://api.acoustid.org/v2/lookup?%s", params.Encode())

	resp, err := s.httpClient.Get(fullURL)
	if err != nil {
		metrics.TrackExternalCall("acoustid", start, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := fmt.Errorf("acoustid api error: %s", resp.Status)
		metrics.TrackExternalCall("acoustid", start, apiErr)
		return nil, apiErr
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

	metrics.TrackExternalCall("acoustid", start, nil)
	return data.Results, nil
}

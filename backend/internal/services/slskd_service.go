package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

type SlskdService struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewSlskdService(cfg *config.Config) *SlskdService {
	return &SlskdService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (s *SlskdService) Search(query string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/searches", s.cfg.SlskdURL)
	payload := map[string]interface{}{
		"searchText": query,
	}
	
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slskd search failed: %s", resp.Status)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ID, nil
}

func (s *SlskdService) GetSearchResults(searchID string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/searches/%s/responses", s.cfg.SlskdURL, searchID)
	
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}

func (s *SlskdService) Download(username, filename string) error {
	// Implementation for triggering download in slskd
	return nil
}

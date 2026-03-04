package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

type DownloadState string

const (
	DownloadStateQueued       DownloadState = "Queued"
	DownloadStateInitializing DownloadState = "Initializing"
	DownloadStateInProgress   DownloadState = "InProgress"
	DownloadStateCompleted    DownloadState = "Completed"
	DownloadStateCancelled    DownloadState = "Cancelled"
	DownloadStateErrored      DownloadState = "Errored"
)

type SearchResult struct {
	Username    string  `json:"username"`
	Filename    string  `json:"filename"`
	Size        int64   `json:"size"`
	Speed       int     `json:"uploadSpeed"`
	QueueLength int     `json:"queueLength"`
	Locked      bool    `json:"isLocked"`
	Bitrate     *int    `json:"bitRate"`
	Length      *int    `json:"length"`
	Score       float64 `json:"-"`
}

func (r *SearchResult) CalculateScore() {
	score := 0.0

	// Prefer higher bitrate
	if r.Bitrate != nil {
		if *r.Bitrate >= 320 {
			score += 10
		} else if *r.Bitrate >= 256 {
			score += 7
		} else if *r.Bitrate >= 192 {
			score += 5
		}
	}

	// Prefer faster users
	if r.Speed > 0 {
		speedScore := float64(r.Speed) / 1000000.0
		if speedScore > 5.0 {
			speedScore = 5.0
		}
		score += speedScore
	}

	// Penalize queue length
	queuePenalty := float64(r.QueueLength) / 10.0
	if queuePenalty > 3.0 {
		queuePenalty = 3.0
	}
	score -= queuePenalty

	// Penalize locked files
	if r.Locked {
		score -= 5
	}

	r.Score = score
}

type Download struct {
	ID              string        `json:"id"`
	Username        string        `json:"username"`
	Filename        string        `json:"filename"`
	Size            int64         `json:"size"`
	State           DownloadState `json:"state"`
	BytesDownloaded int64         `json:"bytesDownloaded"`
	AverageSpeed    float64       `json:"averageSpeed"`
	Path            string        `json:"path"`
}

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

func (s *SlskdService) Search(query string, timeout int) ([]SearchResult, error) {
	url := fmt.Sprintf("%s/api/v0/searches", s.cfg.SlskdURL)
	payload := map[string]interface{}{
		"searchText":      query,
		"searchTimeout":   timeout * 1000,
		"filterResponses": true,
	}
	
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slskd search initiation failed: %s", resp.Status)
	}

	var startResult struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResult); err != nil {
		return nil, err
	}

	// Wait for search to gather results
	time.Sleep(time.Duration(timeout) * time.Second)

	// Fetch results
	resultsURL := fmt.Sprintf("%s/api/v0/searches/%s", s.cfg.SlskdURL, startResult.ID)
	req, _ = http.NewRequest("GET", resultsURL, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)

	resp, err = s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var resultsData struct {
		Responses []struct {
			Username    string `json:"username"`
			UploadSpeed int    `json:"uploadSpeed"`
			QueueLength int    `json:"queueLength"`
			Files       []struct {
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
				IsLocked bool   `json:"isLocked"`
				BitRate  *int   `json:"bitRate"`
				Length   *int   `json:"length"`
			} `json:"files"`
		} `json:"responses"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&resultsData); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, res := range resultsData.Responses {
		for _, f := range res.Files {
			sr := SearchResult{
				Username:    res.Username,
				Filename:    f.Filename,
				Size:        f.Size,
				Speed:       res.UploadSpeed,
				QueueLength: res.QueueLength,
				Locked:      f.IsLocked,
				Bitrate:     f.BitRate,
				Length:      f.Length,
			}
			sr.CalculateScore()
			results = append(results, sr)
		}
	}

	// Sort by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

func (s *SlskdService) EnqueueDownload(username, filename string) (string, error) {
	url := fmt.Sprintf("%s/api/v0/downloads", s.cfg.SlskdURL)
	payload := map[string]interface{}{
		"username": username,
		"files":    []string{filename},
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("slskd download enqueue failed: %s", resp.Status)
	}

	return fmt.Sprintf("%s:%s", username, filename), nil
}

func (s *SlskdService) GetDownload(username, filename string) (*Download, error) {
	url := fmt.Sprintf("%s/api/v0/downloads/%s/%s", s.cfg.SlskdURL, username, url.PathEscape(filename))
	
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	var d Download
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}

	return &d, nil
}

func (s *SlskdService) HealthCheck() bool {
	url := fmt.Sprintf("%s/api/v0/session", s.cfg.SlskdURL)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

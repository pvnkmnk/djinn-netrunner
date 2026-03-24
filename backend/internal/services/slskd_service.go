package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
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

func (r *SearchResult) CalculateScore(profile *database.QualityProfile) {
	score := 0.0

	// 1. Bitrate Scoring
	if r.Bitrate != nil {
		bitrate := *r.Bitrate
		if bitrate >= 1000 { // Likely FLAC/Lossless
			score += 25
		} else if bitrate >= 320 {
			score += 15
		} else if bitrate >= 256 {
			score += 10
		} else if bitrate >= 128 {
			score += 5
		} else {
			score -= 20 // Low quality penalty
		}
	}

	// 2. Profile Match Scoring
	format := ""
	if dotIndex := strings.LastIndex(r.Filename, "."); dotIndex != -1 {
		format = strings.ToLower(r.Filename[dotIndex+1:])
	}

	if profile != nil {
		bitrate := 0
		if r.Bitrate != nil {
			bitrate = *r.Bitrate
		}

		if profile.IsMatch(format, bitrate) {
			score += 30 // Heavy bonus for matching profile
		} else {
			score -= 60 // Heavy penalty for not matching profile
		}

		// Prefer lossless if profile says so
		isLossless := strings.EqualFold(format, "flac") || strings.EqualFold(format, "wav")
		if profile.PreferLossless && isLossless {
			score += 20
		}
	}

	// 3. User Speed & Reliability
	if r.Speed > 0 {
		speedMbps := float64(r.Speed) / 1024.0 / 1024.0
		if speedMbps > 10.0 {
			score += 10
		} else if speedMbps > 1.0 {
			score += 5
		} else if speedMbps < 0.1 {
			score -= 10 // Very slow user penalty
		}
	}

	// 4. Queue Penalty (Non-linear)
	if r.QueueLength > 0 {
		if r.QueueLength > 50 {
			score -= 30
		} else if r.QueueLength > 10 {
			score -= 15
		} else {
			score -= float64(r.QueueLength)
		}
	} else {
		score += 10 // Empty queue bonus
	}

	// 5. Keyword Analysis
	filenameLower := strings.ToLower(r.Filename)
	if strings.Contains(filenameLower, "sample") || strings.Contains(filenameLower, "preview") {
		score -= 100 // Exclude samples
	}
	if strings.Contains(filenameLower, "remix") || strings.Contains(filenameLower, "edit") {
		score -= 5 // Slight penalty for non-original if looking for canonical
	}
	if strings.Contains(filenameLower, "official") || strings.Contains(filenameLower, "digital") {
		score += 5
	}

	// 6. Locked Penalty
	if r.Locked {
		score -= 50 // We usually can't download locked files anyway
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
	cfg         *config.Config
	httpClient  *http.Client
	rateLimiter *searchRateLimiter
}

// searchRateLimiter enforces slskd's search rate limit (34 searches per 220 seconds).
type searchRateLimiter struct {
	tokens chan struct{}
}

func newSearchRateLimiter() *searchRateLimiter {
	rl := &searchRateLimiter{
		tokens: make(chan struct{}, 34),
	}
	// Fill initial tokens
	for i := 0; i < 34; i++ {
		rl.tokens <- struct{}{}
	}
	// Refill 34 tokens every 220 seconds
	go func() {
		ticker := time.NewTicker(220 * time.Second)
		for range ticker.C {
			for i := 0; i < 34; i++ {
				select {
				case rl.tokens <- struct{}{}:
				default:
				}
			}
		}
	}()
	return rl
}

func (rl *searchRateLimiter) Wait() {
	<-rl.tokens
}

func NewSlskdService(cfg *config.Config) *SlskdService {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return &SlskdService{
		cfg:         cfg,
		httpClient:  client,
		rateLimiter: newSearchRateLimiter(),
	}
}

func (s *SlskdService) Search(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
	// Rate limit: wait for an available search token (34 per 220 seconds)
	s.rateLimiter.Wait()

	// If profile has a search suffix (e.g., "flac"), append it to query
	if profile != nil {
		suffix := profile.GetSearchSuffix()
		if suffix != "" {
			query = fmt.Sprintf("%s %s", query, suffix)
		}
	}

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
			sr.CalculateScore(profile)
			results = append(results, sr)
		}
	}

	// Sort by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Clean up search to free resources on slskd
	s.deleteSearch(startResult.ID)

	return results, nil
}

// deleteSearch removes a completed search from slskd.
func (s *SlskdService) deleteSearch(searchID string) {
	url := fmt.Sprintf("%s/api/v0/searches/%s", s.cfg.SlskdURL, searchID)
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)
	resp, err := s.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
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
	u := fmt.Sprintf("%s/api/v0/downloads/%s/%s", s.cfg.SlskdURL, username, url.PathEscape(filename))

	req, _ := http.NewRequest("GET", u, nil)
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

func (s *SlskdService) WaitForDownload(username, filename string, timeout time.Duration) (*Download, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	start := time.Now()
	for {
		select {
		case <-ticker.C:
			if time.Since(start) > timeout {
				return nil, fmt.Errorf("download timeout")
			}

			d, err := s.GetDownload(username, filename)
			if err != nil {
				return nil, err
			}
			if d == nil {
				return nil, fmt.Errorf("download not found")
			}

			if d.State == DownloadStateCompleted {
				return d, nil
			}
			if d.State == DownloadStateCancelled || d.State == DownloadStateErrored {
				return nil, fmt.Errorf("download failed: %s", d.State)
			}
		}
	}
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

// PeerFile represents a file shared by a peer (from browse response).
type PeerFile struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Bitrate  *int   `json:"bitRate"`
}

// Browse retrieves the shared files of a peer.
// Calls GET /api/v0/users/{username}/browse on slskd.
func (s *SlskdService) Browse(username string) ([]PeerFile, error) {
	url := fmt.Sprintf("%s/api/v0/users/%s/browse", s.cfg.SlskdURL, url.PathEscape(username))
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browse request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browse returned %d", resp.StatusCode)
	}

	var result struct {
		Directories []struct {
			Name  string `json:"name"`
			Files []struct {
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
				BitRate  *int   `json:"bitRate"`
			} `json:"files"`
		} `json:"directories"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode browse response: %w", err)
	}

	var files []PeerFile
	for _, dir := range result.Directories {
		for _, f := range dir.Files {
			files = append(files, PeerFile{
				Filename: f.Filename,
				Size:     f.Size,
				Bitrate:  f.BitRate,
			})
		}
	}
	return files, nil
}

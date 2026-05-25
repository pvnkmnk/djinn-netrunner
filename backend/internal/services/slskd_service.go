package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type DownloadState string

const (
	DownloadStateQueued       DownloadState = "Queued"
	DownloadStateRequested    DownloadState = "Requested"
	DownloadStateInitializing DownloadState = "Initializing"
	DownloadStateInProgress   DownloadState = "InProgress"
	DownloadStateCompleted    DownloadState = "Completed"
	DownloadStateSucceeded    DownloadState = "Succeeded"
	DownloadStateCancelled    DownloadState = "Cancelled"
	DownloadStateTimedOut     DownloadState = "TimedOut"
	DownloadStateErrored      DownloadState = "Errored"
	DownloadStateRejected     DownloadState = "Rejected"
)

// IsTerminal returns true if the state indicates a completed transfer.
func (s DownloadState) IsTerminal() bool {
	return strings.Contains(string(s), string(DownloadStateCompleted))
}

// IsSucceeded returns true if the transfer completed successfully.
func (s DownloadState) IsSucceeded() bool {
	return strings.Contains(string(s), string(DownloadStateCompleted)) &&
		strings.Contains(string(s), string(DownloadStateSucceeded))
}

// IsFailed returns true if the transfer completed with an error, cancellation, timeout, or rejection.
func (s DownloadState) IsFailed() bool {
	if !s.IsTerminal() {
		return false
	}
	s2 := string(s)
	return strings.Contains(s2, string(DownloadStateErrored)) ||
		strings.Contains(s2, string(DownloadStateCancelled)) ||
		strings.Contains(s2, string(DownloadStateTimedOut)) ||
		strings.Contains(s2, string(DownloadStateRejected))
}

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

// ApplyPeerReputation adjusts a SearchResult's score based on the peer's
// historical reliability. Called after CalculateScore.
func (r *SearchResult) ApplyPeerReputation(rep *database.PeerReputation) {
	if rep == nil {
		return
	}

	// Ignore peers with consistently poor track record
	if rep.IsIgnored() {
		r.Score -= 200
		return
	}

	// New or low-sample peers get neutral treatment
	if rep.TotalDownloads < 3 {
		return
	}

	// Adjust score based on success rate
	rate := rep.SuccessRate()
	if rate >= 0.95 {
		r.Score += 15 // Reliable peer bonus
	} else if rate >= 0.80 {
		r.Score += 5 // Decent peer bonus
	} else if rate < 0.50 {
		r.Score -= 30 // Unreliable peer penalty
	}

	// Speed bonus from historical data
	if rep.AvgSpeed > 5*1024*1024 { // >5 MB/s
		r.Score += 10
	} else if rep.AvgSpeed > 1*1024*1024 { // >1 MB/s
		r.Score += 5
	} else if rep.AvgSpeed < 100*1024 { // <100 KB/s
		r.Score -= 10
	}
}

type Download struct {
	ID               string        `json:"id"`
	Username         string        `json:"username"`
	Filename         string        `json:"filename"`
	Size             int64         `json:"size"`
	State            DownloadState `json:"state"`
	BytesTransferred int64         `json:"bytesTransferred"`
	BytesRemaining   int64         `json:"bytesRemaining"`
	PercentComplete  float64       `json:"percentComplete"`
	AverageSpeed     float64       `json:"averageSpeed"`
	StartedAt        *time.Time    `json:"startedAt"`
	EndedAt          *time.Time    `json:"endedAt"`
	Exception        string        `json:"exception"`

	// LocalPath is computed by NetRunner from the download staging directory.
	LocalPath string `json:"-"`
}

type SlskdService struct {
	cfg         *config.Config
	httpClient  *http.Client
	rateLimiter *searchRateLimiter
	db          *gorm.DB
}

// searchRateLimiter enforces slskd's search rate limit (34 searches per 220 seconds).
type searchRateLimiter struct {
	tokens    chan struct{}
	stop      chan struct{}
	closeOnce sync.Once
}

func newSearchRateLimiter() *searchRateLimiter {
	rl := &searchRateLimiter{
		tokens: make(chan struct{}, 34),
		stop:   make(chan struct{}),
	}
	// Fill initial tokens
	for i := 0; i < 34; i++ {
		rl.tokens <- struct{}{}
	}
	// Refill 34 tokens every 220 seconds; stops when stop channel is closed
	go func() {
		ticker := time.NewTicker(220 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-rl.stop:
				return
			case <-ticker.C:
				for i := 0; i < 34; i++ {
					select {
					case rl.tokens <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return rl
}

func (rl *searchRateLimiter) Stop() {
	rl.closeOnce.Do(func() { close(rl.stop) })
}

func (rl *searchRateLimiter) Wait() {
	<-rl.tokens
}

// Stop shuts down the background rate limiter goroutine.
func (s *SlskdService) Stop() {
	s.rateLimiter.Stop()
}

func NewSlskdService(cfg *config.Config, db *gorm.DB) *SlskdService {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			slog.Warn("Invalid PROXY_URL, running without proxy", "error", err)
		} else {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return &SlskdService{
		cfg:         cfg,
		httpClient:  client,
		rateLimiter: newSearchRateLimiter(),
		db:          db,
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

	// Fetch results (includeResponses=true is required to get file data)
	resultsURL := fmt.Sprintf("%s/api/v0/searches/%s?includeResponses=true", s.cfg.SlskdURL, startResult.ID)
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

	// Batch fetch peer reputations to avoid N+1 query problem
	peerReputations := make(map[string]database.PeerReputation)
	if s.db != nil {
		usernames := make([]string, 0, len(resultsData.Responses))
		seen := make(map[string]bool)
		for _, res := range resultsData.Responses {
			if !seen[res.Username] {
				usernames = append(usernames, res.Username)
				seen[res.Username] = true
			}
		}
		if len(usernames) > 0 {
			var reps []database.PeerReputation
			s.db.Where("username IN ?", usernames).Find(&reps)
			for _, rep := range reps {
				peerReputations[rep.Username] = rep
			}
		}
	}

	var results []SearchResult
	for _, res := range resultsData.Responses {
		// Get reputation once per user (not per file)
		rep, hasRep := peerReputations[res.Username]

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

			// Apply peer reputation adjustment using pre-fetched data
			if hasRep {
				sr.ApplyPeerReputation(&rep)
			}

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

// EnqueueDownload queues a file download via slskd.
// Returns the download ID (GUID) assigned by slskd.
func (s *SlskdService) EnqueueDownload(username, filename string, size int64) (string, error) {
	u := fmt.Sprintf("%s/api/v0/transfers/downloads/%s", s.cfg.SlskdURL, url.PathEscape(username))

	type downloadRequest struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	payload := []downloadRequest{{Filename: filename, Size: size}}

	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", u, bytes.NewBuffer(jsonPayload))
	req.Header.Set("X-API-Key", s.cfg.SlskdAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slskd download enqueue failed: %s", resp.Status)
	}

	// Parse the response to extract the download ID from enqueued transfers.
	var enqueueResp struct {
		Enqueued []Download `json:"enqueued"`
		Failed   []struct {
			Filename string `json:"filename"`
			Message  string `json:"message"`
		} `json:"failed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&enqueueResp); err != nil {
		// If we can't parse the response, return a composite key as fallback.
		return fmt.Sprintf("%s:%s", username, filename), nil
	}

	if len(enqueueResp.Failed) > 0 {
		return "", fmt.Errorf("slskd rejected download: %s", enqueueResp.Failed[0].Message)
	}

	if len(enqueueResp.Enqueued) > 0 {
		return enqueueResp.Enqueued[0].ID, nil
	}

	return fmt.Sprintf("%s:%s", username, filename), nil
}

// GetDownload retrieves a specific download by its GUID.
func (s *SlskdService) GetDownload(username, downloadID string) (*Download, error) {
	u := fmt.Sprintf("%s/api/v0/transfers/downloads/%s/%s",
		s.cfg.SlskdURL, url.PathEscape(username), downloadID)

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
	if resp.StatusCode == http.StatusBadRequest {
		return nil, fmt.Errorf("invalid download ID: %s", downloadID)
	}

	var d Download
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}

	// Compute local path from staging directory and remote filename.
	d.LocalPath = s.resolveDownloadPath(username, d.Filename)

	return &d, nil
}

// resolveDownloadPath constructs the local filesystem path where slskd stores
// a completed download. slskd saves files at:
//   {downloads_dir}/{username}/{remote_path}
// with backslash path separators converted to forward slashes.
func (s *SlskdService) resolveDownloadPath(username, remoteFilename string) string {
	staging := s.cfg.DownloadStagingPath
	if staging == "" {
		staging = "./downloads"
	}
	// Convert Windows-style backslash paths to local forward slashes.
	localRelative := strings.ReplaceAll(remoteFilename, "\\", "/")
	// Remove any leading slashes or @@-prefixed path components.
	localRelative = strings.TrimLeft(localRelative, "/")
	if idx := strings.Index(localRelative, "/"); idx >= 0 && strings.HasPrefix(localRelative, "@@") {
		localRelative = localRelative[idx+1:]
	}
	return filepath.Join(staging, username, localRelative)
}

// WaitForDownload polls slskd until the download identified by downloadID
// reaches a terminal state. It uses the GUID returned by EnqueueDownload.
func (s *SlskdService) WaitForDownload(ctx context.Context, username, downloadID string, timeout time.Duration) (*Download, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	start := time.Now()
	var lastBytes int64
	lastProgress := time.Now()
	stallThreshold := 90 * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Since(start) > timeout {
				return nil, fmt.Errorf("download timeout after %v", timeout)
			}

			d, err := s.GetDownload(username, downloadID)
			if err != nil {
				return nil, err
			}
			if d == nil {
				return nil, fmt.Errorf("download not found (id: %s)", downloadID)
			}

			// slskd uses compound states like "Completed, Succeeded"
			if d.State.IsSucceeded() {
				return d, nil
			}
			if d.State.IsFailed() {
				msg := string(d.State)
				if d.Exception != "" {
					msg = fmt.Sprintf("%s: %s", d.State, d.Exception)
				}
				return nil, fmt.Errorf("download failed: %s", msg)
			}

			// Stalled download detection
			if strings.Contains(string(d.State), string(DownloadStateInProgress)) {
				if d.BytesTransferred > lastBytes {
					lastBytes = d.BytesTransferred
					lastProgress = time.Now()
				} else if time.Since(lastProgress) > stallThreshold {
					return nil, fmt.Errorf("download stalled (no progress for %v)", stallThreshold)
				}
			}
		}
	}
}

func (s *SlskdService) HealthCheck() bool {
	u := fmt.Sprintf("%s/api/v0/session", s.cfg.SlskdURL)
	req, _ := http.NewRequest("GET", u, nil)
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
	u := fmt.Sprintf("%s/api/v0/users/%s/browse", s.cfg.SlskdURL, url.PathEscape(username))
	req, _ := http.NewRequest("GET", u, nil)
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

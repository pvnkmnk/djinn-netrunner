package services

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

type NotificationService struct {
	webhookURL string
	enabled    bool
	client     *http.Client
}

func NewNotificationService(webhookURL string, enabled bool) *NotificationService {
	return &NotificationService{
		webhookURL: webhookURL,
		enabled:    enabled,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type JobCompletionPayload struct {
	JobID       uint64    `json:"job_id"`
	Type        string    `json:"type"`
	State       string    `json:"state"`
	Summary     string    `json:"summary"`
	CompletedAt time.Time `json:"completed_at"`
	WorkerID    string    `json:"worker_id,omitempty"`
}

// QuotaAlertPayload is sent when a library approaches or exceeds its disk quota.
type QuotaAlertPayload struct {
	LibraryID    string `json:"library_id"`
	LibraryName  string `json:"library_name"`
	UsedBytes    int64  `json:"used_bytes"`
	LimitBytes   int64  `json:"limit_bytes"`
	UsedPct      int    `json:"used_pct"`
	ThresholdPct int    `json:"threshold_pct"`
}

func (s *NotificationService) NotifyJobCompletion(jobID uint64, jobType, state, summary, workerID string) {
	if !s.enabled || s.webhookURL == "" {
		return
	}

	payload := JobCompletionPayload{
		JobID:       jobID,
		Type:        jobType,
		State:       state,
		Summary:     summary,
		CompletedAt: time.Now(),
		WorkerID:    workerID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[NOTIFY] Failed to marshal webhook payload: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[NOTIFY] Failed to create webhook request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NetRunner/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[NOTIFY] Webhook POST failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain body

	if resp.StatusCode >= 300 {
		log.Printf("[NOTIFY] Webhook returned non-success status: %d", resp.StatusCode)
		return
	}

	log.Printf("[NOTIFY] Job %d notification sent successfully", jobID)
}

// NotifyQuotaWarning sends a webhook alert when a library exceeds its quota threshold.
func (s *NotificationService) NotifyQuotaWarning(usage *LibraryUsage, thresholdPct int) {
	if !s.enabled || s.webhookURL == "" {
		return
	}

	payload := QuotaAlertPayload{
		LibraryID:    usage.LibraryID,
		LibraryName:  usage.LibraryName,
		UsedBytes:    usage.UsedBytes,
		LimitBytes:   usage.LimitBytes,
		UsedPct:      usage.UsedPct,
		ThresholdPct: thresholdPct,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[NOTIFY] Failed to marshal quota alert payload: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[NOTIFY] Failed to create quota alert request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NetRunner/1.0")
	req.Header.Set("X-Netrunner-Alert", "quota-warning")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[NOTIFY] Quota alert webhook POST failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		log.Printf("[NOTIFY] Quota alert webhook returned non-success status: %d", resp.StatusCode)
		return
	}

	log.Printf("[NOTIFY] Quota warning sent for library %s (%d%%)", usage.LibraryName, usage.UsedPct)
}

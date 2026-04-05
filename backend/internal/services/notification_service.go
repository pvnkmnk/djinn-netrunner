package services

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
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
		slog.Error("Failed to marshal webhook payload", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("Failed to create webhook request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NetRunner/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Error("Webhook POST failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain body

	if resp.StatusCode >= 300 {
		slog.Warn("Webhook returned non-success status", "status", resp.StatusCode)
		return
	}

	slog.Info("Job notification sent", "job_id", jobID)
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
		slog.Error("Failed to marshal quota alert payload", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("Failed to create quota alert request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NetRunner/1.0")
	req.Header.Set("X-Netrunner-Alert", "quota-warning")

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Error("Quota alert webhook POST failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		slog.Warn("Quota alert webhook returned non-success status", "status", resp.StatusCode)
		return
	}

	slog.Warn("Quota warning sent", "library", usage.LibraryName, "used_pct", usage.UsedPct)
}

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

type NotificationService struct {
	webhookURL string
	enabled    bool
	client     *http.Client
}

func NewNotificationService(cfg *config.Config) *NotificationService {
	return &NotificationService{
		webhookURL: cfg.NotificationWebhookURL,
		enabled:    cfg.NotificationEnabled && cfg.NotificationWebhookURL != "",
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

type JobCompletionPayload struct {
	JobID   uint64 `json:"job_id"`
	Type    string `json:"type"`
	State   string `json:"state"`
	Summary string `json:"summary"`
	At      string `json:"completed_at"`
}

func (s *NotificationService) NotifyJobCompletion(job *database.Job) error {
	if !s.enabled {
		return nil
	}

	payload := JobCompletionPayload{
		JobID:   job.ID,
		Type:    job.Type,
		State:   job.State,
		Summary: job.Summary,
		At:      time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("[NOTIFY] Webhook returned status %d for job %d", resp.StatusCode, job.ID)
		return nil // Don't retry, just log
	}

	log.Printf("[NOTIFY] Sent completion notification for job %d", job.ID)
	return nil
}

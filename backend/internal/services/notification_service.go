package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"time"
)

type NotificationService struct {
	webhookURL string
	enabled    bool
	client     *http.Client

	// SMTP transport
	smtpHost    string
	smtpPort    string
	smtpUser    string
	smtpPass    string
	smtpFrom    string
	smtpEnabled bool
}

func NewNotificationService(webhookURL string, enabled bool, httpClient *http.Client) *NotificationService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &NotificationService{
		webhookURL: webhookURL,
		enabled:    enabled,
		client:     httpClient,
	}
}

// ConfigureSMTP sets up SMTP email transport for notifications.
func (s *NotificationService) ConfigureSMTP(host, port, user, pass, from string, enabled bool) {
	s.smtpHost = host
	s.smtpPort = port
	s.smtpUser = user
	s.smtpPass = pass
	s.smtpFrom = from
	s.smtpEnabled = enabled
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
	// Webhook delivery (independent of SMTP)
	if s.enabled && s.webhookURL != "" {
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
		} else if req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, s.webhookURL, bytes.NewReader(body)); err != nil {
			slog.Error("Failed to create webhook request", "error", err)
		} else {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "NetRunner/1.0")
			resp, err := s.client.Do(req)
			if err != nil {
				slog.Error("Webhook POST failed", "error", err)
			} else {
				if _, err := io.Copy(io.Discard, resp.Body); err != nil {
					slog.Warn("Failed to drain webhook response body", "error", err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode >= 300 {
					slog.Warn("Webhook returned non-success status", "status", resp.StatusCode)
				} else {
					slog.Info("Job webhook notification sent", "job_id", jobID)
				}
			}
		}
	}

	// SMTP delivery (independent of webhook)
	s.sendJobCompletionEmail(jobID, jobType, state, summary)
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, s.webhookURL, bytes.NewReader(body))
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
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		slog.Warn("Failed to drain quota alert response body", "error", err)
	}

	if resp.StatusCode >= 300 {
		slog.Warn("Quota alert webhook returned non-success status", "status", resp.StatusCode)
		return
	}

	slog.Warn("Quota warning sent", "library", usage.LibraryName, "used_pct", usage.UsedPct)
}

func (s *NotificationService) sendJobCompletionEmail(jobID uint64, jobType, state, summary string) {
	if !s.smtpEnabled || s.smtpHost == "" || s.smtpFrom == "" {
		return
	}

	subject := fmt.Sprintf("[NetRunner] Job #%d %s: %s", jobID, state, jobType)
	body := fmt.Sprintf("Job #%d (%s) completed with state: %s\n\nSummary: %s\n\nTime: %s",
		jobID, jobType, state, summary, time.Now().Format(time.RFC1123))

	s.sendEmail(s.smtpFrom, subject, body)
}

func (s *NotificationService) sendEmail(to, subject, body string) {
	addr := fmt.Sprintf("%s:%s", s.smtpHost, s.smtpPort)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.smtpFrom, to, subject, body)

	var auth smtp.Auth
	if s.smtpUser != "" {
		auth = smtp.PlainAuth("", s.smtpUser, s.smtpPass, s.smtpHost)
	}

	err := smtp.SendMail(addr, auth, s.smtpFrom, []string{to}, []byte(msg))
	if err != nil {
		slog.Error("SMTP email send failed", "to", to, "error", err)
		return
	}
	slog.Info("Email notification sent", "to", to, "subject", subject)
}

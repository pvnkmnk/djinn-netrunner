package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookEndToEnd(t *testing.T) {
	var receivedPayload JobCompletionPayload
	var callCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewNotificationService(server.URL, true)

	tests := []struct {
		name     string
		jobType  string
		state    string
		summary  string
		workerID string
	}{
		{"sync success", "sync", "succeeded", "10 tracks acquired", "worker-1"},
		{"scan success", "scan", "succeeded", "500 tracks indexed", "worker-2"},
		{"sync failure", "sync", "failed", "slskd connection timeout", "worker-1"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset payload before each call since the variable is shared
			receivedPayload = JobCompletionPayload{}

			jobID := uint64(100 + i)

			svc.NotifyJobCompletion(jobID, tt.jobType, tt.state, tt.summary, tt.workerID)

			if receivedPayload.JobID != jobID {
				t.Errorf("expected JobID %d, got %d", jobID, receivedPayload.JobID)
			}
			if receivedPayload.Type != tt.jobType {
				t.Errorf("expected Type %s, got %s", tt.jobType, receivedPayload.Type)
			}
			if receivedPayload.State != tt.state {
				t.Errorf("expected State %s, got %s", tt.state, receivedPayload.State)
			}
			if receivedPayload.Summary != tt.summary {
				t.Errorf("expected Summary %s, got %s", tt.summary, receivedPayload.Summary)
			}
			if receivedPayload.WorkerID != tt.workerID {
				t.Errorf("expected WorkerID %s, got %s", tt.workerID, receivedPayload.WorkerID)
			}
		})
	}
}

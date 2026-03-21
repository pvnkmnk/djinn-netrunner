package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotificationService_NotifyJobCompletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var receivedPayload JobCompletionPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		svc.NotifyJobCompletion(42, "sync", "succeeded", "Completed", "worker-1")

		if receivedPayload.JobID != 42 {
			t.Errorf("expected JobID 42, got %d", receivedPayload.JobID)
		}
		if receivedPayload.Type != "sync" {
			t.Errorf("expected Type sync, got %s", receivedPayload.Type)
		}
		if receivedPayload.State != "succeeded" {
			t.Errorf("expected State succeeded, got %s", receivedPayload.State)
		}
		if receivedPayload.WorkerID != "worker-1" {
			t.Errorf("expected WorkerID worker-1, got %s", receivedPayload.WorkerID)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Should not panic, just log error
		svc := NewNotificationService(server.URL, true)
		svc.NotifyJobCompletion(1, "sync", "failed", "error", "worker-1")
	})

	t.Run("disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("server should not be called when disabled")
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, false)
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})

	t.Run("empty url", func(t *testing.T) {
		svc := NewNotificationService("", true)
		// Should not panic
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})

	t.Run("validates CompletedAt is set", func(t *testing.T) {
		var receivedPayload JobCompletionPayload
		var reqTime time.Time
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}
			reqTime = time.Now()
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, true)
		svc.NotifyJobCompletion(99, "download", "completed", "Done", "test-worker")

		// CompletedAt should be set and within a reasonable window
		if receivedPayload.CompletedAt.IsZero() {
			t.Error("expected CompletedAt to be set")
		}
		// Should be within 5 seconds of request time
		diff := receivedPayload.CompletedAt.Sub(reqTime)
		if diff < -5*time.Second || diff > 5*time.Second {
			t.Errorf("CompletedAt seems incorrect: %v (request time: %v, diff: %v)", receivedPayload.CompletedAt, reqTime, diff)
		}
	})
}

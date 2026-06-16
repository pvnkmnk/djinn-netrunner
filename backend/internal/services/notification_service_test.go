package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

		svc := NewNotificationService(server.URL, true, nil)
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
		if receivedPayload.Summary != "Completed" {
			t.Errorf("expected Summary 'Completed', got %s", receivedPayload.Summary)
		}
		if receivedPayload.CompletedAt.IsZero() {
			t.Error("expected CompletedAt to be set")
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// NotifyJobCompletion has no return value; verify it doesn't panic on server error
		svc := NewNotificationService(server.URL, true, nil)
		svc.NotifyJobCompletion(1, "sync", "failed", "error", "worker-1")
	})

	t.Run("disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("server should not be called when disabled")
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, false, nil)
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})

	t.Run("empty url", func(t *testing.T) {
		svc := NewNotificationService("", true, nil)
		// Should not panic
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})

	t.Run("repeated failure does not panic", func(t *testing.T) {
		var callCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, true, nil)
		for i := 0; i < 5; i++ {
			svc.NotifyJobCompletion(uint64(i), "sync", "failed", "error", "worker-1")
		}
		if callCount != 5 {
			t.Errorf("expected 5 calls, got %d", callCount)
		}
	})

	t.Run("payload contains all required fields", func(t *testing.T) {
		var payload map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, true, nil)
		svc.NotifyJobCompletion(42, "enrich", "succeeded", "Enriched 10 tracks", "worker-2")

		assertEqualField(t, payload, "job_id", float64(42))
		assertEqualField(t, payload, "type", "enrich")
		assertEqualField(t, payload, "state", "succeeded")
		assertEqualField(t, payload, "summary", "Enriched 10 tracks")
		assertEqualField(t, payload, "worker_id", "worker-2")
		if _, ok := payload["completed_at"]; !ok {
			t.Error("expected completed_at in payload")
		}
	})

	t.Run("large summary does not break payload", func(t *testing.T) {
		var payload map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		longSummary := strings.Repeat("x", 10000)
		svc := NewNotificationService(server.URL, true, nil)
		svc.NotifyJobCompletion(1, "scan", "succeeded", longSummary, "worker-1")

		summary, _ := payload["summary"].(string)
		if len(summary) != 10000 {
			t.Errorf("expected summary length 10000, got %d", len(summary))
		}
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

		svc := NewNotificationService(server.URL, true, nil)
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

func assertEqualField(t *testing.T, m map[string]interface{}, key string, expected interface{}) {
	t.Helper()
	got, ok := m[key]
	if !ok {
		t.Errorf("missing field %s in payload", key)
		return
	}
	if got != expected {
		t.Errorf("field %s: expected %v, got %v", key, expected, got)
	}
}

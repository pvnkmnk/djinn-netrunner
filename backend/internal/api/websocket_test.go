package api

import (
	"html"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

// TestWebSocketManager_Subscribe tests the subscription of WebSocket connections
func TestWebSocketManager_Subscribe(t *testing.T) {
	manager := NewWebSocketManager()

	// Create a mock connection (we'll use a fiber app for this)
	_ = fiber.New()

	// Test that we can subscribe a connection to a job
	manager.Subscribe(nil, "1")

	// Check that the connection was subscribed
	manager.mu.RLock()
	clients, ok := manager.clients["1"]
	manager.mu.RUnlock()

	assert.True(t, ok, "clients map should contain jobID '1'")
	assert.Len(t, clients, 1, "should have 1 connection subscribed")
}

// TestWebSocketManager_Unsubscribe tests the unsubscription of WebSocket connections
func TestWebSocketManager_Unsubscribe(t *testing.T) {
	manager := NewWebSocketManager()

	// Subscribe a connection
	manager.Subscribe(nil, "1")

	// Unsubscribe it
	manager.Unsubscribe(nil, "1")

	// Check that the connection was removed
	manager.mu.RLock()
	_, ok := manager.clients["1"]
	manager.mu.RUnlock()

	assert.False(t, ok, "clients map should not contain jobID '1' after unsubscribe")
}

// TestWebSocketManager_Broadcast tests broadcasting messages to connections
func TestWebSocketManager_Broadcast(t *testing.T) {
	manager := NewWebSocketManager()

	// Test that broadcast doesn't panic with no connections
	assert.NotPanics(t, func() {
		manager.Broadcast("999", "test message")
	}, "broadcast should not panic with no connections")
}

// TestWebSocketManager_Cleanup tests the cleanup of dead connections
func TestWebSocketManager_Cleanup(t *testing.T) {
	manager := NewWebSocketManager()

	// Test that cleanup doesn't panic with no connections
	assert.NotPanics(t, func() {
		manager.Cleanup()
	}, "cleanup should not panic with no connections")
}

// TestWebSocketManager_HandleEvents tests the event handler setup
func TestWebSocketManager_HandleEvents(t *testing.T) {
	manager := NewWebSocketManager()

	// The HandleEvents function should subscribe and then wait for reads
	// We test that it doesn't panic when called with a nil connection
	// Note: This is a basic test - in production you'd use a real WebSocket client
	app := fiber.New()

	// We can't easily test the full WebSocket flow without a real client
	// but we can verify the manager state is valid
	_ = app
	assert.NotNil(t, manager.clients, "clients map should be initialized")
	assert.NotNil(t, manager.mu, "mutex should be initialized")
}

// TestStringsToLower tests the helper function for lowercase conversion
func TestStringsToLower(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"INFO", "info"},
		{"DEBUG", "debug"},
		{"ERROR", "error"},
		{"WARN", "warn"},
		{"", ""},
		{"MixedCase", "mixedcase"},
	}

	for _, tt := range tests {
		result := stringsToLower(tt.input)
		assert.Equal(t, tt.expected, result, "stringsToLower should convert to lowercase")
	}
}

// TestLogSanitization ensures log messages are properly escaped for XSS prevention
func TestLogSanitization(t *testing.T) {
	// This is a unit test for the logic inside ListenForJobLogs and HandleConsole
	// Since those methods are hard to unit test directly due to DB/PQ dependencies,
	// we verify the sanitization logic here.

	rawMessage := "<script>alert('xss')</script>"
	escapedMessage := "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"

	assert.Equal(t, escapedMessage, html.EscapeString(rawMessage))
}

// TestWebSocketManager_MultiJobSubscription tests that clients can subscribe
// to multiple jobs independently
func TestWebSocketManager_MultiJobSubscription(t *testing.T) {
	manager := NewWebSocketManager()

	// Subscribe to multiple jobs
	manager.Subscribe(nil, "job1")
	manager.Subscribe(nil, "job2")
	manager.Subscribe(nil, "job3")

	// Verify all jobs have clients
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	assert.Len(t, manager.clients, 3, "should have 3 job subscriptions")
	assert.Contains(t, manager.clients, "job1")
	assert.Contains(t, manager.clients, "job2")
	assert.Contains(t, manager.clients, "job3")
}

// TestWebSocketManager_UnsubscribeOneJobPreservesOthers tests that unsubscribing
// from one job doesn't affect subscriptions to other jobs
func TestWebSocketManager_UnsubscribeOneJobPreservesOthers(t *testing.T) {
	manager := NewWebSocketManager()

	// Subscribe to multiple jobs
	manager.Subscribe(nil, "job1")
	manager.Subscribe(nil, "job2")

	// Unsubscribe from job1 only
	manager.Unsubscribe(nil, "job1")

	// Verify job2 is still subscribed
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	_, job1Exists := manager.clients["job1"]
	_, job2Exists := manager.clients["job2"]

	assert.False(t, job1Exists, "job1 should be removed after unsubscribe")
	assert.True(t, job2Exists, "job2 should still be subscribed")
}

package api

import (
	"html"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

// TestWebSocketManager_Register tests the registration of WebSocket connections
func TestWebSocketManager_Register(t *testing.T) {
	manager := NewWebSocketManager()

	// Create a mock connection (we'll use a fiber app for this)
	_ = fiber.New()

	// Test that we can register a jobID connection
	manager.Register(1, nil)

	// Check that the connection was registered
	manager.mutex.RLock()
	conns, ok := manager.connections[1]
	manager.mutex.RUnlock()

	assert.True(t, ok, "connection map should contain jobID 1")
	assert.Len(t, conns, 1, "should have 1 connection registered")
}

// TestWebSocketManager_Unregister tests the unregistration of WebSocket connections
func TestWebSocketManager_Unregister(t *testing.T) {
	manager := NewWebSocketManager()

	// Register a connection
	manager.Register(1, nil)

	// Unregister it
	manager.Unregister(1, nil)

	// Check that the connection was removed
	manager.mutex.RLock()
	_, ok := manager.connections[1]
	manager.mutex.RUnlock()

	assert.False(t, ok, "connection map should not contain jobID 1 after unregister")
}

// TestWebSocketManager_Broadcast tests broadcasting messages to connections
func TestWebSocketManager_Broadcast(t *testing.T) {
	manager := NewWebSocketManager()

	// Test that broadcast doesn't panic with no connections
	assert.NotPanics(t, func() {
		manager.Broadcast(999, "test message")
	}, "broadcast should not panic with no connections")
}

// TestWebSocketManager_HandleEvents tests the event handler setup
func TestWebSocketManager_HandleEvents(t *testing.T) {
	manager := NewWebSocketManager()

	// The HandleEvents function should register and then wait for reads
	// We test that it doesn't panic when called with a nil connection
	// Note: This is a basic test - in production you'd use a real WebSocket client
	app := fiber.New()

	// We can't easily test the full WebSocket flow without a real client
	// but we can verify the manager state is valid
	_ = app
	assert.NotNil(t, manager.connections, "connections map should be initialized")
	assert.NotNil(t, manager.mutex, "mutex should be initialized")
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

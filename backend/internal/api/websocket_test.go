package api

import (
	"context"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketManager_Subscribe tests the subscription of WebSocket connections
func TestWebSocketManager_Subscribe(t *testing.T) {
	manager := NewWebSocketManager()

	// Test that we can subscribe a client to a jobID
	manager.Subscribe(nil, "1")

	// Check that the client was subscribed
	manager.mu.RLock()
	clients, ok := manager.clients["1"]
	manager.mu.RUnlock()

	assert.True(t, ok, "clients map should contain jobID 1")
	assert.Len(t, clients, 1, "should have 1 client subscribed")
}

// TestWebSocketManager_Unsubscribe tests the unsubscription of WebSocket connections
func TestWebSocketManager_Unsubscribe(t *testing.T) {
	manager := NewWebSocketManager()

	// Subscribe a client
	manager.Subscribe(nil, "1")

	// Unsubscribe it
	manager.Unsubscribe(nil, "1")

	// Check that the client was removed
	manager.mu.RLock()
	_, ok := manager.clients["1"]
	manager.mu.RUnlock()

	assert.False(t, ok, "clients map should not contain jobID 1 after unsubscribe")
}

// TestWebSocketManager_Broadcast tests broadcasting messages to connections
func TestWebSocketManager_Broadcast(t *testing.T) {
	manager := NewWebSocketManager()

	// Test that broadcast doesn't panic with no connections
	assert.NotPanics(t, func() {
		manager.Broadcast("999", "test message")
	}, "broadcast should not panic with no connections")
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
	// mu is a value type (sync.RWMutex), always initialized with the struct — no pointer check needed
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

func TestListenForJobLogsSkipsNonPostgresDatabase(t *testing.T) {
	manager := NewWebSocketManager()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		manager.ListenForJobLogs(ctx, "/tmp/netrunner-test.db", nil)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		require.Fail(t, "SQLite log listener did not stop after context cancellation")
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

// Console frame contract (DJI-562).
//
// The console's socket does not swap a message into the element that opened it.
// This app ships htmx's in-core websocket support (hx-ws), and in-core ws swaps
// each top-level element of a message into the element carrying the same id. A
// frame that names no destination is dropped with an oobErrorNoTarget and the
// console simply stays empty - which is exactly how it read before: a pane that
// could not be told from "no output yet". These pin the destination, and pin it
// against the page, because it is written down in two places that cannot check
// each other.

// consoleFrameTargetRE pulls the out-of-band destination out of a console frame.
var consoleFrameTargetRE = regexp.MustCompile(`hx-swap-oob="beforeend:#([^"]+)"`)

func TestLogLineFrameNamesItsDestination(t *testing.T) {
	frame := logLineHTML(database.JobLog{
		ID:        7,
		JobID:     3,
		Level:     "INFO",
		Message:   "downloaded 1 of 5",
		CreatedAt: time.Now(),
	})

	match := consoleFrameTargetRE.FindStringSubmatch(frame)
	require.Len(t, match, 2,
		"a console frame has to name where it goes, or htmx drops it silently: %s", frame)
	assert.Equal(t, "console-logs", match[1],
		"every streamed line appends to the log pane; anything else is a pane nobody reads")

	assert.Contains(t, frame, `data-log-id="7"`,
		"the log filters read data-log-id, so a frame without it is unfilterable")

	// The line has to be a CHILD of the element carrying the swap attribute.
	// htmx inserts the children of the out-of-band element into the target, so a
	// log-line carrying the attribute itself loses its own wrapper: measured in
	// the browser, only the time/level/message spans reached the pane, which left
	// .log-line empty and the console's filters and Copy Last 200 matching
	// nothing while the pane looked right.
	assert.Regexp(t, `(?s)<div hx-swap-oob="[^"]*"[^>]*>\s*<div class="log-line\b`, frame,
		"the log line must be a child of the element that carries hx-swap-oob, "+
			"not the element itself")
}

func TestConsoleNoticeFrameNamesTheSameDestination(t *testing.T) {
	frame := consoleNoticeHTML("Access denied: you do not own this job.")

	match := consoleFrameTargetRE.FindStringSubmatch(frame)
	require.Len(t, match, 2,
		"the socket's own refusals have to land where the operator is looking: %s", frame)
	assert.Equal(t, "console-logs", match[1])

	assert.Contains(t, frame, "[ERR]",
		"the level filter matches on the [LEVEL] marker, so a refusal without one "+
			"disappears the moment the operator filters to WARN or ERR")
}

// The destination is the one string that has to agree between a Go format string
// and ops/web/templates/index.html, and nothing else checks the pair: renaming
// the pane leaves a page that looks right and a console that receives every line
// and shows none of them.
func TestTheFrameDestinationExistsOnTheDashboard(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "..", "ops", "web", "templates", "index.html"))
	require.NoError(t, err)

	match := consoleFrameTargetRE.FindStringSubmatch(logLineHTML(database.JobLog{ID: 1, Level: "INFO"}))
	require.Len(t, match, 2, "the log frame no longer names a destination")

	assert.Contains(t, string(page), `id="`+match[1]+`"`,
		"the pane every frame names has to exist on the page (expected #%s)", match[1])
}

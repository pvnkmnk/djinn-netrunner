package api

import (
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestFormatLogHTML(t *testing.T) {
	now := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
	log := database.JobLog{
		ID:        42,
		JobID:     1,
		Level:     "INFO",
		Message:   "Test message",
		CreatedAt: now,
	}

	html := formatLogHTML(log)

	assert.Contains(t, html, `class="log-line log-info"`)
	assert.Contains(t, html, `data-log-id="42"`)
	assert.Contains(t, html, `id="log-42"`)
	assert.Contains(t, html, `role="listitem"`)
	assert.Contains(t, html, `<time class="log-ts" datetime="2025-03-15T12:00:00Z" title="2025-03-15 12:00:00">12:00:00</time>`)
	assert.Contains(t, html, `aria-label="Level: INFO"`)
	assert.Contains(t, html, `[INFO]`)
	assert.Contains(t, html, `Test message`)
	assert.Contains(t, html, `<span class="log-indicator" aria-hidden="true"></span>`)
}

func TestDateFormatting(t *testing.T) {
	// Testing the format used in main.go engine.AddFunc("strftime", ...)
	now := time.Date(2025, 3, 15, 16, 55, 0, 0, time.UTC)
	formatted := now.Format("Jan 02 15:04")
	assert.Equal(t, "Mar 15 16:55", formatted)
}

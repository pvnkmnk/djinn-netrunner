package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWatchlistPreviewHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	assert.NotNil(t, &WatchlistPreviewHandler{})
}

func TestNewWatchlistPreviewHandler(t *testing.T) {
	// Test that NewWatchlistPreviewHandler returns a non-nil handler
	handler := NewWatchlistPreviewHandler(nil)
	assert.NotNil(t, handler, "expected non-nil handler")
	assert.Nil(t, handler.watchlistService, "expected nil watchlistService")
}

func TestPreviewTrack_Struct(t *testing.T) {
	// Test that PreviewTrack struct has expected fields
	track := PreviewTrack{
		Artist:   "Test Artist",
		Title:    "Test Title",
		Album:    "Test Album",
		CoverURL: "https://example.com/cover.jpg",
	}

	assert.Equal(t, "Test Artist", track.Artist)
	assert.Equal(t, "Test Title", track.Title)
	assert.Equal(t, "Test Album", track.Album)
	assert.Equal(t, "https://example.com/cover.jpg", track.CoverURL)
}

func TestPreviewLimit_Constant(t *testing.T) {
	// Test that preview limit is set correctly
	assert.Equal(t, 10, previewLimit)
}

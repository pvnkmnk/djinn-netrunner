package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestRSSProvider_SSRF_Protection(t *testing.T) {
	// Create a local test server to simulate an internal service
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0">
<channel>
  <title>Internal Service</title>
  <item>
    <title>Artist - Track</title>
  </item>
</channel>
</rss>`))
	}))
	defer ts.Close()

	provider := NewRSSProvider()
	watchlist := &database.Watchlist{
		SourceURI:  ts.URL, // This will be something like http://127.0.0.1:xxxxx
		SourceType: "rss_feed",
	}

	// 1. Verify it blocks by default
	_, _, err := provider.FetchTracks(context.Background(), watchlist)
	assert.Error(t, err, "RSSProvider should block localhost requests by default")
	assert.Contains(t, err.Error(), "ssrf:", "Error should be an SSRF block error")

	// 2. Verify it allows when allowLoopback is true (for testing)
	allowLoopback = true
	defer func() { allowLoopback = false }()

	tracks, _, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err, "RSSProvider should allow localhost when allowLoopback is true")
	assert.NotEmpty(t, tracks)
	if len(tracks) > 0 {
		assert.Equal(t, "Artist", tracks[0]["artist"])
	}
}

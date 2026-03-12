package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestRSSProvider_FetchTracks(t *testing.T) {
	// Mock Bandcamp-style RSS feed
	mockRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>New Releases</title>
    <item>
      <title>Artist Name - Track Name</title>
      <pubDate>Wed, 11 Mar 2026 10:00:00 +0000</pubDate>
      <media:content url="http://example.com/cover.jpg" />
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(mockRSS))
	}))
	defer server.Close()

	provider := &RSSProvider{}

	watchlist := &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  server.URL,
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.NotEmpty(t, snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Artist Name", tracks[0]["artist"])
	assert.Equal(t, "Track Name", tracks[0]["title"])
	assert.Equal(t, "http://example.com/cover.jpg", tracks[0]["cover_art_url"])
}

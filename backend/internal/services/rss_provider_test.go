package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Artist One - Track Alpha</title>
      <link>https://example.com/1</link>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Artist Two - Track Beta</title>
      <link>https://example.com/2</link>
      <pubDate>Tue, 02 Jan 2024 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

func TestRSSProvider_UsesCustomHTTPClient(t *testing.T) {
	var requestReceived bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	p := NewRSSProvider(srv.Client())
	wl := &database.Watchlist{SourceType: "rss_feed", SourceURI: srv.URL}

	tracks, snap, err := p.FetchTracks(context.Background(), wl)
	require.NoError(t, err)
	assert.True(t, requestReceived, "custom HTTP client should have been used")
	assert.Len(t, tracks, 2)
	assert.NotEmpty(t, snap)
	assert.Equal(t, "Artist One", tracks[0]["artist"])
	assert.Equal(t, "Track Alpha", tracks[0]["title"])
}

func TestRSSProvider_NilClientStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	p := NewRSSProvider(nil)
	wl := &database.Watchlist{SourceType: "rss_feed", SourceURI: srv.URL}

	tracks, _, err := p.FetchTracks(context.Background(), wl)
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestRSSProvider_ValidateConfig(t *testing.T) {
	provider := &RSSProvider{}

	assert.NoError(t, provider.ValidateConfig("http://example.com/feed.xml"))
	assert.NoError(t, provider.ValidateConfig("https://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("ftp://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("not-a-url"))
	assert.Error(t, provider.ValidateConfig(""))
}

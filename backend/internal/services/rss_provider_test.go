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

func TestRSSProvider_MalformedFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`this is not valid xml`))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	_, _, err := provider.FetchTracks(context.Background(), watchlist)
	assert.Error(t, err)
}

func TestRSSProvider_EmptyFeed(t *testing.T) {
	emptyRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(emptyRSS))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.NotEmpty(t, snap)
	assert.Empty(t, tracks)
}

func TestRSSProvider_NetworkError(t *testing.T) {
	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: "http://127.0.0.1:1/nonexistent"}
	_, _, err := provider.FetchTracks(context.Background(), watchlist)
	assert.Error(t, err)
}

func TestRSSProvider_NoMediaContent(t *testing.T) {
	rssNoMedia := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>New Releases</title>
    <item>
      <title>Artist Name - Track Name</title>
      <pubDate>Wed, 11 Mar 2026 10:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(rssNoMedia))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.NotEmpty(t, snap)
	require.Len(t, tracks, 1)
	assert.Equal(t, "Artist Name", tracks[0]["artist"])
	assert.Equal(t, "Track Name", tracks[0]["title"])
	assert.Empty(t, tracks[0]["cover_art_url"])
}

func TestRSSProvider_MultipleItems(t *testing.T) {
	multiRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
   <channel>
     <title>Multi Release</title>
     <item>
       <title>Artist One - Track One</title>
       <pubDate>Wed, 11 Mar 2026 10:00:00 +0000</pubDate>
     </item>
     <item>
       <title>Artist Two - Track Two</title>
       <pubDate>Wed, 11 Mar 2026 11:00:00 +0000</pubDate>
     </item>
     <item>
       <title>Artist Three - Track Three</title>
       <pubDate>Wed, 11 Mar 2026 12:00:00 +0000</pubDate>
     </item>
   </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(multiRSS))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.NotEmpty(t, snap)
	require.Len(t, tracks, 3)
	assert.Equal(t, "Artist One", tracks[0]["artist"])
	assert.Equal(t, "Track One", tracks[0]["title"])
	assert.Equal(t, "Artist Two", tracks[1]["artist"])
	assert.Equal(t, "Track Two", tracks[1]["title"])
	assert.Equal(t, "Artist Three", tracks[2]["artist"])
	assert.Equal(t, "Track Three", tracks[2]["title"])
}

// TestRSSProvider_BandcampFeed tests Bandcamp-style RSS feed with track-only titles
func TestRSSProvider_BandcampFeed(t *testing.T) {
	// Mock Bandcamp-style RSS feed with track-only titles and enclosure for audio
	bandcampRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>Test Artist</title>
    <item>
      <title>Track One</title>
      <pubDate>Wed, 11 Mar 2026 10:00:00 +0000</pubDate>
      <enclosure url="http://example.com/track1.mp3" length="12345" type="audio/mpeg" />
    </item>
    <item>
      <title>Track Two</title>
      <pubDate>Wed, 11 Mar 2026 11:00:00 +0000</pubDate>
      <enclosure url="http://example.com/track2.mp3" length="67890" type="audio/mpeg" />
    </item>
    <!-- Also test mixed format: one item with Artist - Title -->
    <item>
      <title>Known Artist - Known Track</title>
      <pubDate>Wed, 11 Mar 2026 12:00:00 +0000</pubDate>
      <media:content url="http://example.com/cover.jpg" />
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(bandcampRSS))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.NotEmpty(t, snap)
	require.Len(t, tracks, 3)

	// First track: track-only title should use channel title as artist
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Track One", tracks[0]["title"])
	// Note: enclosure URL is not currently captured in cover_art_url (that's for media:content)
	// But we verify the parsing didn't fail

	// Second track: track-only title should use channel title as artist
	assert.Equal(t, "Test Artist", tracks[1]["artist"])
	assert.Equal(t, "Track Two", tracks[1]["title"])

	// Third track: Artist - Title format should work normally
	assert.Equal(t, "Known Artist", tracks[2]["artist"])
	assert.Equal(t, "Known Track", tracks[2]["title"])
	assert.Equal(t, "http://example.com/cover.jpg", tracks[2]["cover_art_url"])
}

// TestRSSProvider_HTTPError tests HTTP error handling (404)
func TestRSSProvider_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404
		w.Write([]byte(`not found`))
	}))
	defer server.Close()

	provider := &RSSProvider{}
	watchlist := &database.Watchlist{SourceType: "rss_feed", SourceURI: server.URL}
	_, _, err := provider.FetchTracks(context.Background(), watchlist)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse RSS feed")
}

package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestProviders_SSRFProtection(t *testing.T) {
	// Start a local server that should be BLOCKED
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("internal data"))
	}))
	defer server.Close()

	ctx := context.Background()

	t.Run("RSSProvider_SSRF", func(t *testing.T) {
		provider := NewRSSProvider()
		watchlist := &database.Watchlist{
			SourceType: "rss_feed",
			SourceURI:  server.URL,
		}
		_, _, err := provider.FetchTracks(ctx, watchlist)
		assert.Error(t, err, "Expected RSSProvider to block local URL")
		assert.Contains(t, err.Error(), "ssrf: no public IP found", "Expected SSRF error message")
	})

	t.Run("LastFMProvider_SSRF", func(t *testing.T) {
		provider := NewLastFMProvider("fake_key")
		provider.BaseURL = server.URL
		watchlist := &database.Watchlist{
			SourceType: "lastfm_loved",
			SourceURI:  "user123",
		}
		_, _, err := provider.FetchTracks(ctx, watchlist)
		assert.Error(t, err, "Expected LastFMProvider to block local URL")
		assert.Contains(t, err.Error(), "ssrf: no public IP found", "Expected SSRF error message")
	})

	t.Run("ListenBrainzProvider_SSRF", func(t *testing.T) {
		provider := NewListenBrainzProvider("fake_token")
		provider.BaseURL = server.URL
		watchlist := &database.Watchlist{
			SourceType: "listenbrainz_listens",
			SourceURI:  "user123",
		}
		_, _, err := provider.FetchTracks(ctx, watchlist)
		assert.Error(t, err, "Expected ListenBrainzProvider to block local URL")
		assert.Contains(t, err.Error(), "ssrf: no public IP found", "Expected SSRF error message")
	})

	t.Run("DiscogsProvider_SSRF", func(t *testing.T) {
		provider := NewDiscogsProvider("fake_token")
		provider.BaseURL = server.URL
		watchlist := &database.Watchlist{
			SourceType: "discogs_wantlist",
			SourceURI:  "user123",
		}
		_, _, err := provider.FetchTracks(ctx, watchlist)
		assert.Error(t, err, "Expected DiscogsProvider to block local URL")
		assert.Contains(t, err.Error(), "ssrf: no public IP found", "Expected SSRF error message")
	})
}

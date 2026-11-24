package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
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

func TestRSSProvider_NilClientUsesStrictClient(t *testing.T) {
	var feedRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	p := NewRSSProvider(nil)
	wl := &database.Watchlist{SourceType: "rss_feed", SourceURI: srv.URL}

	_, _, err := p.FetchTracks(context.Background(), wl)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDisallowedDestination)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindRefused, providerErr.Kind)
	assert.Zero(t, feedRequests.Load(), "nil-client construction must not fetch a private RSS URL")
}

func TestRSSProvider_CallerCancellationIsNotNetworkFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := NewRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})

	_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://feed.example.test/rss",
	})
	require.Error(t, err)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindCanceled, providerErr.Kind)
	assert.NotEqual(t, ErrKindNetwork, providerErr.Kind)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRSSProvider_CallerDeadlineIsNotNetworkFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	provider := NewRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})

	_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://feed.example.test/rss",
	})
	require.Error(t, err)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindDeadline, providerErr.Kind)
	assert.NotEqual(t, ErrKindNetwork, providerErr.Kind)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

type rssRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f rssRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWatchlistService_RSSSourceIgnoresPrivateTargetBypassAndProxy(t *testing.T) {
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testRSSFeed))
	}))
	defer proxy.Close()

	var feedRequests atomic.Int64
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testRSSFeed))
	}))
	defer feed.Close()

	service := NewWatchlistService(nil, nil, &config.Config{
		AllowPrivateTargets: true,
		ProxyURL:            proxy.URL,
	})
	_, _, err := service.FetchWatchlistTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  feed.URL,
	})

	require.Error(t, err, "RSS must remain guarded even when the global bypass and proxy are configured")
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindRefused, providerErr.Kind)
	assert.ErrorIs(t, err, ErrDisallowedDestination)
	assert.Contains(t, err.Error(), "ssrf: target")
	assert.Zero(t, feedRequests.Load(), "the private RSS source must not be dialed")
	assert.Zero(t, proxyRequests.Load(), "the private RSS URL must never reach the proxy")
}

func TestRSSProvider_PublicToPrivateRedirectNeverProxiesPrivateTarget(t *testing.T) {
	const publicFeedURL = "http://8.8.8.8/feed.xml"
	const privateRedirectURL = "http://127.0.0.1:8080/internal"
	var proxiedURLs []string
	proxy := rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		proxiedURLs = append(proxiedURLs, req.URL.String())
		response := &http.Response{
			StatusCode: http.StatusFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}
		response.Header.Set("Location", privateRedirectURL)
		return response, nil
	})
	provider := NewRSSProvider(newStrictRSSHTTPClientWithTransport(5*time.Second, proxy))
	_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  publicFeedURL,
	})

	require.Error(t, err, "a public RSS URL must not redirect to a private destination")
	assert.Contains(t, err.Error(), "redirect target 127.0.0.1")
	assert.Equal(t, []string{publicFeedURL}, proxiedURLs,
		"the proxy may receive the public feed request but must never receive the private redirect")
}

func TestRSSProvider_ValidateConfig(t *testing.T) {
	provider := &RSSProvider{}

	assert.NoError(t, provider.ValidateConfig("http://example.com/feed.xml"))
	assert.NoError(t, provider.ValidateConfig("https://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("ftp://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("not-a-url"))
	assert.Error(t, provider.ValidateConfig(""))
}

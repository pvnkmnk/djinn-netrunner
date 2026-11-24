package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func newTestRSSProvider(client *http.Client) *RSSProvider {
	return newTestRSSProviderWithResolver(client, nil)
}

func newTestRSSProviderWithResolver(client *http.Client, resolver *net.Resolver) *RSSProvider {
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := client.Transport
	if transport == nil {
		transport = safeTransport
	}
	strictClient := newStrictRSSHTTPClientWithResolver(timeout, transport, resolver)
	return &RSSProvider{httpClient: withRSSFeedSizeLimit(strictClient)}
}

func TestRSSProvider_UsesInjectedTransportBehindStrictClient(t *testing.T) {
	var requestReceived bool
	p := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requestReceived = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body:       io.NopCloser(strings.NewReader(testRSSFeed)),
			Request:    req,
		}, nil
	})})
	wl := &database.Watchlist{SourceType: "rss_feed", SourceURI: "http://8.8.8.8/feed.xml"}

	tracks, snap, err := p.FetchTracks(context.Background(), wl)
	require.NoError(t, err)
	assert.True(t, requestReceived, "the injected test transport should run after strict destination validation")
	assert.Len(t, tracks, 2)
	assert.NotEmpty(t, snap)
	assert.Equal(t, "Artist One", tracks[0]["artist"])
	assert.Equal(t, "Track Alpha", tracks[0]["title"])
}

func TestRSSProvider_RejectsDeclaredOversizedResponse(t *testing.T) {
	provider := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(testRSSFeed)),
			ContentLength: int64(maxRSSFeedSize + 1),
			Request:       req,
		}, nil
	})})

	_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/rss",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errRSSFeedTooLarge)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindUpstream, providerErr.Kind)
}

func TestRSSProvider_RejectsStreamingOversizedResponse(t *testing.T) {
	feed := `<?xml version="1.0"?><rss version="2.0"><channel><title>` +
		strings.Repeat("x", maxRSSFeedSize) +
		`</title></channel></rss>`
	provider := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(feed)),
			ContentLength: -1,
			Request:       req,
		}, nil
	})})

	_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/rss",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errRSSFeedTooLarge)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindUpstream, providerErr.Kind)
}

func TestRSSProvider_CancellationTakesPrecedenceOverOversizedResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(testRSSFeed)),
			ContentLength: int64(maxRSSFeedSize + 1),
			Request:       req,
		}, nil
	})})

	_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/rss",
	})
	require.Error(t, err)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindCanceled, providerErr.Kind)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRSSProvider_RejectsGzipExpansionOverLimitThroughHTTPTransport(t *testing.T) {
	expanded := []byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>` +
		strings.Repeat("x", maxRSSFeedSize) +
		`</title></channel></rss>`)
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	_, err := gzipWriter.Write(expanded)
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())
	require.Greater(t, len(expanded), maxRSSFeedSize)
	require.Less(t, compressed.Len(), maxRSSFeedSize)

	requestedGzip := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedGzip.Store(r.Header.Get("Accept-Encoding") == "gzip")
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressed.Bytes())
	}))
	defer srv.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	localAddress := srv.Listener.Addr().String()
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, localAddress)
	}
	defer transport.CloseIdleConnections()

	provider := newTestRSSProvider(&http.Client{Timeout: 5 * time.Second, Transport: transport})
	_, _, err = provider.FetchTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/feed.xml",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errRSSFeedTooLarge)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindUpstream, providerErr.Kind)
	assert.True(t, requestedGzip.Load(), "the real HTTP transport should negotiate gzip before the cap sees the expanded body")
}

func TestRSSProvider_NilClientUsesStrictClient(t *testing.T) {
	var feedRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	p := NewRSSProvider()
	wl := &database.Watchlist{SourceType: "rss_feed", SourceURI: srv.URL}

	_, _, err := p.FetchTracks(context.Background(), wl)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDisallowedDestination)
	var providerErr *ProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, ErrKindRefused, providerErr.Kind)
	assert.Zero(t, feedRequests.Load(), "the provider constructor must not fetch a private RSS URL")
}

func TestRSSProvider_ZeroValueAndNilReceiverUseStrictClient(t *testing.T) {
	var feedRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testRSSFeed))
	}))
	defer srv.Close()

	for _, tt := range []struct {
		name     string
		provider *RSSProvider
	}{
		{name: "zero value", provider: &RSSProvider{}},
		{name: "nil receiver", provider: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.provider.FetchTracks(context.Background(), &database.Watchlist{
				SourceType: "rss_feed",
				SourceURI:  srv.URL,
			})
			require.ErrorIs(t, err, ErrDisallowedDestination)
			var providerErr *ProviderError
			require.ErrorAs(t, err, &providerErr)
			assert.Equal(t, ErrKindRefused, providerErr.Kind)
			assert.Zero(t, feedRequests.Load(), "an unconfigured provider must not use gofeed's default client")
		})
	}
}

func TestRSSProvider_CallerCancellationIsNotNetworkFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})

	_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/rss",
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
	provider := newTestRSSProvider(&http.Client{Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})})

	_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/rss",
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

func newStalledRSSResolver() (*net.Resolver, <-chan struct{}) {
	dialStarted := make(chan struct{}, 1)
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			select {
			case dialStarted <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	return resolver, dialStarted
}

func TestRSSProvider_CancelsInflightRequestDNSLookup(t *testing.T) {
	resolver, dialStarted := newStalledRSSResolver()
	var roundTrips atomic.Int64
	provider := newTestRSSProviderWithResolver(&http.Client{
		Timeout: 5 * time.Second,
		Transport: rssRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			roundTrips.Add(1)
			return nil, io.EOF
		}),
	}, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
			SourceType: "rss_feed",
			SourceURI:  "http://stalled.example.test/feed.xml",
		})
		done <- err
	}()

	select {
	case <-dialStarted:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("RSS request did not enter the stalled DNS lookup")
	}
	select {
	case err := <-done:
		require.Error(t, err)
		var providerErr *ProviderError
		require.ErrorAs(t, err, &providerErr)
		assert.Equal(t, ErrKindCanceled, providerErr.Kind)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("RSS request did not stop after its DNS lookup was canceled")
	}
	assert.Zero(t, roundTrips.Load(), "the feed transport must not run after request DNS cancellation")
}

func TestRSSProvider_CancelsInflightRedirectDNSLookup(t *testing.T) {
	resolver, dialStarted := newStalledRSSResolver()
	const publicFeedURL = "http://8.8.8.8/feed.xml"
	var requestedURLs []string
	provider := newTestRSSProviderWithResolver(&http.Client{
		Timeout: 5 * time.Second,
		Transport: rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requestedURLs = append(requestedURLs, req.URL.String())
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://stalled.example.test/private"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := provider.FetchTracks(ctx, &database.Watchlist{
			SourceType: "rss_feed",
			SourceURI:  publicFeedURL,
		})
		done <- err
	}()

	select {
	case <-dialStarted:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("RSS redirect did not enter the stalled DNS lookup")
	}
	select {
	case err := <-done:
		require.Error(t, err)
		var providerErr *ProviderError
		require.ErrorAs(t, err, &providerErr)
		assert.Equal(t, ErrKindCanceled, providerErr.Kind)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("RSS redirect did not stop after its DNS lookup was canceled")
	}
	assert.Equal(t, []string{publicFeedURL}, requestedURLs,
		"the redirect target must not reach the transport after redirect DNS cancellation")
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

func TestWatchlistService_PublicRSSFetchBypassesConfiguredProxy(t *testing.T) {
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

	service := NewWatchlistService(nil, nil, &config.Config{ProxyURL: proxy.URL})
	provider, ok := service.providers["rss_feed"].(*RSSProvider)
	require.True(t, ok, "watchlist service should register the strict RSS provider")
	limitTransport, ok := provider.httpClient.Transport.(*rssFeedSizeLimitTransport)
	require.True(t, ok, "RSS responses should pass through the size limiter")
	validator, ok := limitTransport.next.(*safeAddressValidator)
	require.True(t, ok, "RSS requests should pass through destination validation")
	configuredTransport, ok := validator.next.(*http.Transport)
	require.True(t, ok, "RSS provider should use the direct HTTP transport")
	assert.Nil(t, configuredTransport.Proxy, "RSS must not inherit the configured PROXY_URL")

	// Route the otherwise-public IP to the local fixture through a real HTTP
	// transport so this successful production-wiring path stays deterministic.
	localTransport := http.DefaultTransport.(*http.Transport).Clone()
	localTransport.Proxy = nil
	localAddress := feed.Listener.Addr().String()
	dialer := &net.Dialer{}
	localTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, localAddress)
	}
	defer localTransport.CloseIdleConnections()
	validator.next = localTransport

	tracks, _, err := service.FetchWatchlistTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  "http://8.8.8.8/feed.xml",
	})
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
	assert.Equal(t, int64(1), feedRequests.Load(), "the direct test transport should fetch the public feed")
	assert.Zero(t, proxyRequests.Load(), "a public RSS URL must not reach the configured proxy")
}

func TestRSSProvider_PublicToPrivateRedirectNeverProxiesPrivateTarget(t *testing.T) {
	const publicFeedURL = "http://8.8.8.8/feed.xml"
	var privateRequests atomic.Int64
	privateTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateRequests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testRSSFeed))
	}))
	defer privateTarget.Close()
	privateRedirectURL := privateTarget.URL + "/internal"

	var proxiedURLs []string
	proxy := rssRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		proxiedURLs = append(proxiedURLs, req.URL.String())
		if req.URL.String() != publicFeedURL {
			// Model a proxy that would forward a leaked private destination.
			return http.DefaultTransport.RoundTrip(req)
		}
		response := &http.Response{
			StatusCode: http.StatusFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}
		response.Header.Set("Location", privateRedirectURL)
		return response, nil
	})
	provider := newTestRSSProvider(&http.Client{Timeout: 5 * time.Second, Transport: proxy})
	_, _, err := provider.FetchTracks(context.Background(), &database.Watchlist{
		SourceType: "rss_feed",
		SourceURI:  publicFeedURL,
	})

	require.Error(t, err, "a public RSS URL must not redirect to a private destination")
	assert.Contains(t, err.Error(), "redirect target 127.0.0.1")
	assert.Equal(t, []string{publicFeedURL}, proxiedURLs,
		"the proxy may receive the public feed request but must never receive the private redirect")
	assert.Zero(t, privateRequests.Load(), "the private redirect target must not receive a request")
}

func TestRSSProvider_ValidateConfig(t *testing.T) {
	provider := &RSSProvider{}

	assert.NoError(t, provider.ValidateConfig("http://example.com/feed.xml"))
	assert.NoError(t, provider.ValidateConfig("https://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("ftp://example.com/feed.xml"))
	assert.Error(t, provider.ValidateConfig("not-a-url"))
	assert.Error(t, provider.ValidateConfig(""))
}

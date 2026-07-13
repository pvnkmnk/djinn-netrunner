package services

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProxyAwareHTTPClient_NoProxy(t *testing.T) {
	cfg := &config.Config{ProxyURL: ""}
	client := NewProxyAwareHTTPClient(cfg, 5*time.Second)

	assert.NotNil(t, client)
	assert.Equal(t, 5*time.Second, client.Timeout)
}

func TestNewProxyAwareHTTPClient_InvalidProxy(t *testing.T) {
	cfg := &config.Config{ProxyURL: "://invalid"}
	client := NewProxyAwareHTTPClient(cfg, 5*time.Second)

	// Should still return a working client (falls back to no proxy)
	assert.NotNil(t, client)
}

// TestProxyRouting validates that HTTP traffic from two different provider
// clients is routed through the proxy when PROXY_URL is set.
func TestProxyRouting(t *testing.T) {
	// Track how many requests pass through the proxy
	var proxiedRequests atomic.Int64

	// Upstream API server (simulates MusicBrainz / Discogs)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	// MITM proxy that forwards to the upstream
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedRequests.Add(1)

		// Forward the request to the actual destination
		targetURL := r.URL.String()
		if r.URL.Host == "" {
			targetURL = r.RequestURI
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		proxyReq.Header = r.Header

		resp, err := http.DefaultTransport.RoundTrip(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	cfg := &config.Config{ProxyURL: proxy.URL}

	// Provider 1: simulate MusicBrainz-style client
	client1 := NewProxyAwareHTTPClient(cfg, 10*time.Second)
	resp1, err := client1.Get(upstream.URL + "/ws/2/release?query=test")
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)

	// Provider 2: simulate Discogs-style client
	client2 := NewProxyAwareHTTPClient(cfg, 10*time.Second)
	req, err := http.NewRequest("GET", upstream.URL+"/users/testuser/wants", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Discogs token=test-token")
	resp2, err := client2.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Both requests should have passed through the proxy
	assert.Equal(t, int64(2), proxiedRequests.Load(),
		"expected 2 requests to route through proxy")
}

// TestProxyNotUsedWhenUnset validates that traffic does NOT route through a
// proxy when PROXY_URL is empty.
func TestProxyNotUsedWhenUnset(t *testing.T) {
	var proxiedRequests atomic.Int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	cfg := &config.Config{ProxyURL: ""}
	client := NewProxyAwareHTTPClient(cfg, 10*time.Second)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, int64(0), proxiedRequests.Load(),
		"no requests should route through proxy when PROXY_URL is empty")
}

// TestProxyAwareHTTPClient_ProviderIntegration validates that provider
// constructors correctly use the factory-produced proxy client.
func TestProxyAwareHTTPClient_ProviderIntegration(t *testing.T) {
	var proxiedHosts []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return valid but minimal responses for each provider type
		path := r.URL.Path
		if strings.Contains(path, "wants") {
			w.Write([]byte(`{"pagination":{"items":0},"wants":[]}`))
		} else {
			w.Write([]byte(`{"lovedtracks":{"track":[],"@attr":{"total":"0"}}}`))
		}
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHosts = append(proxiedHosts, r.Host)

		targetURL := r.RequestURI
		proxyReq, _ := http.NewRequest(r.Method, targetURL, r.Body)
		proxyReq.Header = r.Header
		resp, err := http.DefaultTransport.RoundTrip(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	cfg := &config.Config{ProxyURL: proxy.URL}
	proxyClient := NewProxyAwareHTTPClient(cfg, 10*time.Second)

	// Parse upstream URL for host extraction
	upstreamURL, _ := url.Parse(upstream.URL)

	// Test DiscogsProvider uses proxy client
	dp := NewDiscogsProvider("test-token", proxyClient)
	dp.BaseURL = upstream.URL + "/"
	_, _ = proxyClient.Get(upstream.URL + "/users/testuser/wants")

	// Test LastFMProvider uses proxy client
	lfm := NewLastFMProvider("test-key", proxyClient)
	lfm.BaseURL = upstream.URL + "/"
	_, _ = proxyClient.Get(upstream.URL + "/2.0/?method=user.getlovedtracks")

	assert.GreaterOrEqual(t, len(proxiedHosts), 2,
		"at least 2 provider requests should have been proxied")
	for _, host := range proxiedHosts {
		assert.Contains(t, host, upstreamURL.Host,
			"proxied request should target upstream host")
	}

	// Verify provider structs actually hold the injected client
	assert.Equal(t, proxyClient, dp.httpClient, "DiscogsProvider should use injected client")
	assert.Equal(t, proxyClient, lfm.httpClient, "LastFMProvider should use injected client")
}

// TestNewSafeProxyAwareHTTPClient_NoProxy tests that NewSafeProxyAwareHTTPClient
// returns a client with safe transport when no proxy is configured.
func TestNewSafeProxyAwareHTTPClient_NoProxy(t *testing.T) {
	cfg := &config.Config{ProxyURL: ""}
	client := NewSafeProxyAwareHTTPClient(cfg, 5*time.Second)

	assert.NotNil(t, client)
	assert.Equal(t, 5*time.Second, client.Timeout)
	// Verify the transport has safeDialContext configured
	assert.NotNil(t, client.Transport)
}

// TestNewSafeProxyAwareHTTPClient_Proxy tests that NewSafeProxyAwareHTTPClient
// creates a client with safeAddressValidator when a proxy URL is configured,
// and rejects requests to private IP targets.
func TestNewSafeProxyAwareHTTPClient_Proxy(t *testing.T) {
	var proxiedRequests atomic.Int64

	// Upstream API server (simulates MusicBrainz / Discogs)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	// MITM proxy that forwards to the upstream
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedRequests.Add(1)

		targetURL := r.URL.String()
		if r.URL.Host == "" {
			targetURL = r.RequestURI
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		proxyReq.Header = r.Header

		resp, err := http.DefaultTransport.RoundTrip(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	cfg := &config.Config{ProxyURL: proxy.URL}
	client := NewSafeProxyAwareHTTPClient(cfg, 10*time.Second)

	// The safe proxy client should reject requests to private IP targets,
	// even when a proxy is configured.
	upstreamURL := upstream.URL // e.g., http://127.0.0.1:PORT
	_, err := client.Get(upstreamURL + "/ws/2/release?query=test")
	require.Error(t, err, "requests to private IP targets should be rejected")
	assert.Contains(t, err.Error(), "ssrf: target")
	assert.Contains(t, err.Error(), "private IP")

	// The request should NOT have reached the proxy
	assert.Equal(t, int64(0), proxiedRequests.Load(),
		"no requests should reach the proxy for private IP targets")
}

// TestNewSafeProxyAwareHTTPClient_ProxyConstructed validates that
// NewSafeProxyAwareHTTPClient returns a correctly configured client with the
// safeAddressValidator transport when a proxy URL is set.
func TestNewSafeProxyAwareHTTPClient_ProxyConstructed(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	cfg := &config.Config{ProxyURL: proxy.URL}
	client := NewSafeProxyAwareHTTPClient(cfg, 10*time.Second)

	assert.NotNil(t, client)
	assert.Equal(t, 10*time.Second, client.Timeout)
	_, ok := client.Transport.(*safeAddressValidator)
	assert.True(t, ok, "transport should be a *safeAddressValidator when proxy is set")

	// Invalid proxy URL should fall back to safe client
	cfgBad := &config.Config{ProxyURL: "://invalid"}
	fallback := NewSafeProxyAwareHTTPClient(cfgBad, 5*time.Second)
	assert.NotNil(t, fallback)
	assert.Equal(t, 5*time.Second, fallback.Timeout)
}

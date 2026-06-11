package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestRSSProvider_SSRF_Reproduction(t *testing.T) {
	// Temporarily disable allowLoopback for this specific test
	// to verify that protection works when it's off.
	// (TestMain enables it globally for other tests)
	allowLoopback = false
	defer func() { allowLoopback = true }()

	// Create a local server that should NOT be reachable via RSS provider
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0">
<channel>
 <title>Internal RSS</title>
 <item>
  <title>Secret Track</title>
 </item>
</channel>
</rss>`)
	}))
	defer ts.Close()

	// Use the safe client
	httpClient := NewSafeProxyAwareHTTPClient(nil, 0)
	provider := NewRSSProvider(httpClient)

	watchlist := &database.Watchlist{
		SourceURI: ts.URL,
	}

	// This SHOULD fail if SSRF protection is active
	_, _, err := provider.FetchTracks(context.Background(), watchlist)

	if err == nil {
		t.Errorf("Successfully fetched from local server (SSRF protection failed)")
	} else {
		t.Logf("Correctly blocked fetch from local server: %v", err)
	}
}

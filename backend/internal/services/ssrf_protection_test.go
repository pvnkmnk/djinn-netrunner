package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestNewSafeProxyAwareHTTPClient_SSRFProtection(t *testing.T) {
	cfg := &config.Config{}
	client := NewSafeProxyAwareHTTPClient(cfg, 2*time.Second)

	// 1. Test blocking private IP (127.0.0.1) by default
	_, err := client.Get("http://127.0.0.1")
	if err == nil {
		t.Error("expected error for private IP 127.0.0.1, got nil")
	}

	// 2. Test blocking local hostname
	_, err = client.Get("http://localhost")
	if err == nil {
		t.Error("expected error for localhost, got nil")
	}

	// 3. Test allowLoopback flag
	allowLoopback = true
	defer func() { allowLoopback = false }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Errorf("expected no error for loopback when allowed, got: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got: %d", resp.StatusCode)
		}
	}
}

func TestNewSafeProxyAwareHTTPClient_RedirectSSRF(t *testing.T) {
	allowLoopback = true
	defer func() { allowLoopback = false }()

	cfg := &config.Config{}
	client := NewSafeProxyAwareHTTPClient(cfg, 2*time.Second)

	// Create a server that redirects to a private IP (10.0.0.1)
	// Even with allowLoopback = true, 10.0.0.1 should be blocked.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/admin", http.StatusFound)
	}))
	defer ts.Close()

	_, err := client.Get(ts.URL)
	if err == nil {
		t.Error("expected error when redirecting to private IP 10.0.0.1, got nil")
	}
}

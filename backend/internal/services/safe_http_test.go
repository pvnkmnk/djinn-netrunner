package services

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fc00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.80.46", false}, // google.com
		{"::ffff:127.0.0.1", true},
		{"::ffff:192.168.1.1", true},
		{"fe80::1", true},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", tt.ip)
		}
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestSafeGet_RejectsPrivateIP(t *testing.T) {
	// localhost should be blocked
	_, err := SafeGet("http://localhost:8080/test")
	if err == nil {
		t.Error("expected error for localhost URL, got nil")
	}

	_, err = SafeGet("http://127.0.0.1:8080/test")
	if err == nil {
		t.Error("expected error for 127.0.0.1 URL, got nil")
	}

	_, err = SafeGet("http://10.0.0.1/test")
	if err == nil {
		t.Error("expected error for 10.0.0.1 URL, got nil")
	}
}

func TestSafeGet_RejectsInvalidScheme(t *testing.T) {
	_, err := SafeGet("file:///etc/passwd")
	if err == nil {
		t.Error("expected error for file:// scheme, got nil")
	}
}

// A loopback httptest server: reachable only via a private/loopback address.
func loopbackTestServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Default safe client must refuse loopback targets (SSRF guard).
func TestSafeClient_BlocksLoopback(t *testing.T) {
	srv := loopbackTestServer(t, http.StatusOK)
	client := NewSafeProxyAwareHTTPClient(&config.Config{}, 5*time.Second)
	resp, err := client.Get(srv.URL + "/health")
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected loopback request to be blocked by SSRF guard, got success")
	}
	if !strings.Contains(err.Error(), "ssrf") {
		t.Errorf("expected ssrf error, got: %v", err)
	}
}

// With AllowPrivateTargets, the same client reaches loopback targets — this is
// the mode the integration harness and docker-network deployments need.
func TestSafeClient_AllowPrivateTargets(t *testing.T) {
	srv := loopbackTestServer(t, http.StatusOK)
	client := NewSafeProxyAwareHTTPClient(&config.Config{AllowPrivateTargets: true}, 5*time.Second)
	resp, err := client.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("expected loopback request to succeed with AllowPrivateTargets, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// The opt-in must not leak into SafeGet (user-influenced URLs stay guarded).
func TestSafeGet_StillGuardedWhenAllowPrivateTargetsSet(t *testing.T) {
	cfg := &config.Config{AllowPrivateTargets: true}
	_ = NewSafeProxyAwareHTTPClient(cfg, time.Second) // construct in allowed mode
	if _, err := SafeGet("http://127.0.0.1:1/test"); err == nil {
		t.Error("expected SafeGet to still block loopback despite AllowPrivateTargets")
	}
}

// No proxy configured + allowance set: plain direct client (no validator).
func TestSafeClient_AllowPrivateTargetsDirectDial(t *testing.T) {
	srv := loopbackTestServer(t, http.StatusOK)
	cfg := &config.Config{AllowPrivateTargets: true, ProxyURL: ""}
	client := NewSafeProxyAwareHTTPClient(cfg, 5*time.Second)
	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Fatalf("expected plain *http.Transport in direct-dial mode, got %T", client.Transport)
	}
	resp, err := client.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	resp.Body.Close()
}

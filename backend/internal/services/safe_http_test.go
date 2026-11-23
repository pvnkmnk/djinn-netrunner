package services

import (
	"context"
	"errors"
	"fmt"
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

// fakeDialer records every address it is asked to dial and answers from a
// per-address script: fail with the mapped error, or return one end of an
// in-memory pipe as a successful "connection".
type fakeDialer struct {
	tried []string
	fail  map[string]error
	peers []net.Conn
}

func (f *fakeDialer) dial(_ context.Context, _ string, address string) (net.Conn, error) {
	f.tried = append(f.tried, address)
	if err, ok := f.fail[address]; ok {
		return nil, err
	}
	c1, c2 := net.Pipe()
	f.peers = append(f.peers, c2)
	return c1, nil
}

func (f *fakeDialer) closePeers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, c := range f.peers {
			c.Close()
		}
	})
}

// The DJI-682 regression: DNS answers an unreachable IPv6 address first and
// a reachable IPv4 second. The old code dialed only the first public IP and
// failed; the fallback must reach the second.
func TestDialPublicIPs_FallsBackToSecondIP(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("2606:4700:4700::1111"), // public IPv6, "unreachable"
		net.ParseIP("1.1.1.1"),              // public IPv4, "reachable"
	}
	d := &fakeDialer{fail: map[string]error{
		"[2606:4700:4700::1111]:443": fmt.Errorf("network is unreachable"),
	}}
	d.closePeers(t)
	conn, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err != nil {
		t.Fatalf("expected fallback to reach the second IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 2 || d.tried[0] != "[2606:4700:4700::1111]:443" || d.tried[1] != "1.1.1.1:443" {
		t.Errorf("tried %v, want both addresses in DNS order", d.tried)
	}
}

// Private IPs are skipped, never dialed, even when interleaved with public ones.
func TestDialPublicIPs_SkipsPrivateIPs(t *testing.T) {
	ips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("1.1.1.1"), net.ParseIP("192.168.1.1")}
	d := &fakeDialer{}
	d.closePeers(t)
	conn, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err != nil {
		t.Fatalf("expected success via the public IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 1 || d.tried[0] != "1.1.1.1:443" {
		t.Errorf("tried %v, want only the public address dialed", d.tried)
	}
}

// No public IP at all: the guard's refusal, not a dial error, and nothing dialed.
func TestDialPublicIPs_AllPrivateRefused(t *testing.T) {
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("10.0.0.1"), net.ParseIP("::1")}
	d := &fakeDialer{}
	_, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err == nil {
		t.Fatal("expected refusal, got nil")
	}
	if !errors.Is(err, ErrDisallowedDestination) {
		t.Errorf("expected ErrDisallowedDestination, got: %v", err)
	}
	if len(d.tried) != 0 {
		t.Errorf("dialed %v, want nothing dialed", d.tried)
	}
}

// Public IPs exist but none dials: a transient error carrying every attempt,
// not a refusal — callers may retry a dial failure, but must not treat it as
// a permanent verdict about the destination.
func TestDialPublicIPs_AllPublicFail(t *testing.T) {
	ips := []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")}
	d := &fakeDialer{fail: map[string]error{
		"1.1.1.1:443": fmt.Errorf("connection refused"),
		"8.8.8.8:443": fmt.Errorf("network is unreachable"),
	}}
	_, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrDisallowedDestination) {
		t.Errorf("dial failure must not be a disallowed-destination refusal, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") || !strings.Contains(err.Error(), "network is unreachable") {
		t.Errorf("expected both dial errors joined, got: %v", err)
	}
	if len(d.tried) != 2 {
		t.Errorf("tried %v, want both addresses attempted", d.tried)
	}
}

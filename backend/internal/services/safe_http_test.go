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

func TestIsNonGlobalIP(t *testing.T) {
	tests := []struct {
		ip        string
		nonGlobal bool
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
		// Shared / reserved / documentation space: IsGlobalUnicast admits
		// these, so the hand list must catch them (Sourcery on #391).
		{"100.64.0.1", true},         // CGNAT
		{"100.127.255.255", true},    // CGNAT
		{"192.0.2.1", true},          // TEST-NET-1
		{"198.51.100.1", true},       // TEST-NET-2
		{"203.0.113.1", true},        // TEST-NET-3
		{"192.0.0.1", true},          // IETF protocol assignments
		{"198.18.0.1", true},         // benchmarking
		{"240.0.0.1", true},          // reserved
		{"255.255.255.255", true},    // broadcast / reserved
		{"224.0.0.1", true},          // multicast
		{"2001:db8::1", true},        // documentation
		{"3fff::1", true},            // documentation, RFC 9637 prefix (Sourcery on #393)
		{"3fff:0fff::1", true},       // top of the 3fff::/20 documentation prefix
		{"3fff:1000::1", false},      // just outside 3fff::/20 — stays dialable
		{"2001::1", true},            // IETF special-purpose (Teredo range)
		{"2002::1", true},            // 6to4
		{"64:ff9b::8.8.8.8", true},   // NAT64 well-known prefix
		{"64:ff9b:1::1", true},       // NAT64 local-use prefix (Sourcery on #392)
		{"64:ff9b:1::8.8.8.8", true}, // NAT64 local-use prefix, embedded IPv4
		{"100::1", true},             // discard-only
		{"fec0::1", true},            // deprecated site-local
		{"ff02::1", true},            // multicast
		{"::", true},                 // unspecified
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.80.46", false}, // google.com
		{"2606:4700:4700::1111", false},
		{"::ffff:8.8.8.8", false},
		{"::ffff:127.0.0.1", true},
		{"::ffff:192.168.1.1", true},
		{"::ffff:100.64.0.1", true},
		{"fe80::1", true},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", tt.ip)
		}
		got := isNonGlobalIP(ip)
		if got != tt.nonGlobal {
			t.Errorf("isNonGlobalIP(%s) = %v, want %v", tt.ip, got, tt.nonGlobal)
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

// CodeRabbit on #394: with no caller deadline, the whole dial sequence gets
// one overall budget instead of a fresh timeout per attempt. A ctx that
// already has a deadline passes through unchanged.
func TestWithOverallDialDeadline(t *testing.T) {
	// No deadline in: one comes out, set to the overall budget.
	ctx, cancel := withOverallDialDeadline(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected an overall deadline on a deadline-less context, got none")
	}
	if remaining := time.Until(deadline); remaining < overallDialTimeout-2*time.Second || remaining > overallDialTimeout {
		t.Errorf("overall deadline is %v out, want ~%v", remaining, overallDialTimeout)
	}

	// Deadline in: the same ctx comes back, untouched.
	want, wantCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wantCancel()
	got, gotCancel := withOverallDialDeadline(want)
	defer gotCancel()
	if got != want {
		t.Error("expected a deadline-bearing context to pass through unchanged")
	}
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

// The Sourcery #391 finding: a DNS answer that mixes a reachable public IP
// with CGNAT / documentation / reserved addresses must dial only the public
// one — the non-global addresses are never attempted, even when they answer
// first.
func TestDialPublicIPs_SkipsNonGlobalIPs(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("100.64.0.1"),       // CGNAT
		net.ParseIP("192.0.2.1"),        // TEST-NET-1
		net.ParseIP("1.1.1.1"),          // public IPv4, "reachable"
		net.ParseIP("2001:db8::1"),      // documentation
		net.ParseIP("240.0.0.1"),        // reserved
		net.ParseIP("64:ff9b::8.8.8.8"), // NAT64 well-known prefix
	}
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

// Only non-global addresses at all: the guard's refusal, not a dial error,
// and nothing dialed — a CGNAT-only answer is a refusal, not a fallback.
func TestDialPublicIPs_AllNonGlobalRefused(t *testing.T) {
	ips := []net.IP{net.ParseIP("100.64.0.1"), net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")}
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

// blockingFirstDialer simulates blackholed addresses: the dial for any
// address in the set waits until its context is cancelled, every other
// address answers from an in-memory pipe.
type blockingFirstDialer struct {
	tried   []string
	blocked map[string]bool
	peers   []net.Conn
}

func (d *blockingFirstDialer) dial(ctx context.Context, _ string, address string) (net.Conn, error) {
	d.tried = append(d.tried, address)
	if d.blocked[address] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c1, c2 := net.Pipe()
	d.peers = append(d.peers, c2)
	return c1, nil
}

func (d *blockingFirstDialer) closePeers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, c := range d.peers {
			c.Close()
		}
	})
}

// The second Sourcery #391 finding: the old loop reused one request context
// for every dial, so a blackholed first IP consumed the whole deadline and
// the reachable second address was never tried. Each attempt now gets a fair
// share of the remaining deadline, so the fallback still happens — well
// inside the parent deadline.
func TestDialPublicIPs_BlackholedFirstIPDoesNotStarveFallback(t *testing.T) {
	ips := []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d := &blockingFirstDialer{blocked: map[string]bool{"1.1.1.1:443": true}}
	d.closePeers(t)
	start := time.Now()
	conn, err := dialPublicIPs(ctx, "tcp", "example.com", "443", ips, d.dial)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected fallback to reach the second IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 2 || d.tried[0] != "1.1.1.1:443" || d.tried[1] != "8.8.8.8:443" {
		t.Errorf("tried %v, want both addresses attempted in DNS order", d.tried)
	}
	// The first attempt's share of a 1s deadline across 2 addresses is ~500ms;
	// the whole fallback must finish well inside the parent deadline. The old
	// code took the full second and failed.
	if elapsed >= 900*time.Millisecond {
		t.Errorf("fallback took %v, want it well inside the 1s parent deadline", elapsed)
	}
}

// The third Sourcery #392 finding: DNS can repeat one address, and the old
// loop counted each copy as a separate target, spending one budget share
// timing out on each. Repeated answers — including the same IPv4 in plain
// and IPv4-mapped form — count once.
func TestDialPublicIPs_DeduplicatesRepeatedIPs(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("1.1.1.1"),
		net.ParseIP("1.1.1.1"),
		net.ParseIP("::ffff:1.1.1.1"),
		net.ParseIP("8.8.8.8"),
		net.ParseIP("8.8.8.8"),
	}
	d := &fakeDialer{fail: map[string]error{
		"1.1.1.1:443": fmt.Errorf("connection refused"),
	}}
	d.closePeers(t)
	conn, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err != nil {
		t.Fatalf("expected fallback to reach the second distinct IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 2 || d.tried[0] != "1.1.1.1:443" || d.tried[1] != "8.8.8.8:443" {
		t.Errorf("tried %v, want each distinct address dialed exactly once", d.tried)
	}
}

// The second Sourcery #392 finding, candidate-set half: a DNS answer with
// more distinct addresses than the fair-share window must not get the whole
// window's budget — when the window's attempts are slow (blackholed), the
// deadline is spent inside the window and the overflow never runs, so
// exactly eight addresses are tried. The literal 8 below is deliberate:
// asserting the intended window size, not the maxDialAttempts constant, so
// raising the constant cannot silently pass this test (Sourcery on #393).
func TestDialPublicIPs_CapsCandidates(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8"), net.ParseIP("9.9.9.9"),
		net.ParseIP("1.0.0.1"), net.ParseIP("8.8.4.4"), net.ParseIP("149.112.112.112"),
		net.ParseIP("208.67.222.222"), net.ParseIP("208.67.220.220"),
		net.ParseIP("194.242.2.2"), net.ParseIP("91.239.100.100"),
	}
	blocked := map[string]bool{}
	for _, ip := range ips {
		blocked[net.JoinHostPort(ip.String(), "443")] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d := &blockingFirstDialer{blocked: blocked}
	d.closePeers(t)
	start := time.Now()
	conn, err := dialPublicIPs(ctx, "tcp", "example.com", "443", ips, d.dial)
	elapsed := time.Since(start)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected failure when every candidate is blackholed, got nil")
	}
	if errors.Is(err, ErrDisallowedDestination) {
		t.Errorf("expected a dial error, not a refusal: %v", err)
	}
	if len(d.tried) != 8 {
		t.Errorf("tried %d addresses, want the 8-attempt fair-share window", len(d.tried))
	}
	// Eight attempts at ~250ms each is ~2s; the loop must finish inside
	// the parent deadline with time to spare for the check, not past it.
	if elapsed >= 2500*time.Millisecond {
		t.Errorf("fallback took %v, want it inside the 2s parent deadline", elapsed)
	}
}

// The Sourcery #393 finding, reachable-later-address half: the window is a
// fair-share window, not a hard cap — when its attempts fail fast and leave
// time on the deadline, a reachable address past the first eight is tried.
func TestDialPublicIPs_OverflowTriesLaterAddress(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8"), net.ParseIP("9.9.9.9"),
		net.ParseIP("1.0.0.1"), net.ParseIP("8.8.4.4"), net.ParseIP("149.112.112.112"),
		net.ParseIP("208.67.222.222"), net.ParseIP("208.67.220.220"),
		net.ParseIP("194.242.2.2"), net.ParseIP("91.239.100.100"),
	}
	fail := map[string]error{}
	for _, ip := range ips[:8] {
		fail[net.JoinHostPort(ip.String(), "443")] = fmt.Errorf("connection refused")
	}
	d := &fakeDialer{fail: fail}
	d.closePeers(t)
	conn, err := dialPublicIPs(context.Background(), "tcp", "example.com", "443", ips, d.dial)
	if err != nil {
		t.Fatalf("expected overflow to reach the ninth IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 9 {
		t.Errorf("tried %v, want the eight window attempts plus the ninth overflow attempt", d.tried)
	}
}

// The second Sourcery #392 finding, budget half: seven blackholed addresses
// ahead of a reachable one must not starve it. With the candidate set
// capped, each of the eight attempts gets a fair share of the 2s deadline,
// so the fallback still reaches the eighth address well inside it.
func TestDialPublicIPs_BlackholedSetDoesNotStarveFallback(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8"), net.ParseIP("9.9.9.9"),
		net.ParseIP("1.0.0.1"), net.ParseIP("8.8.4.4"), net.ParseIP("149.112.112.112"),
		net.ParseIP("208.67.222.222"), net.ParseIP("208.67.220.220"),
	}
	blocked := map[string]bool{}
	for _, ip := range ips[:7] {
		blocked[net.JoinHostPort(ip.String(), "443")] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d := &blockingFirstDialer{blocked: blocked}
	d.closePeers(t)
	start := time.Now()
	conn, err := dialPublicIPs(ctx, "tcp", "example.com", "443", ips, d.dial)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected fallback to reach the eighth IP, got: %v", err)
	}
	defer conn.Close()
	if len(d.tried) != 8 {
		t.Errorf("tried %v, want all eight candidates attempted in DNS order", d.tried)
	}
	// Seven attempts at ~250ms each is ~1.75s; the fallback must finish
	// inside the 2s parent deadline.
	if elapsed >= 1950*time.Millisecond {
		t.Errorf("fallback took %v, want it inside the 2s parent deadline", elapsed)
	}
}

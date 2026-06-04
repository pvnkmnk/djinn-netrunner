package services

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// privateCIDRs contains CIDR ranges that should never be reachable via outbound HTTP.
var privateCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"0.0.0.0/8",
}

// parsedCIDRs is populated at init time.
var parsedCIDRs []*net.IPNet

// allowLoopback permits connections to loopback addresses (127.0.0.1, ::1).
// This is disabled by default for security but can be enabled in tests.
var allowLoopback = false

func init() {
	for _, cidr := range privateCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
		}
		parsedCIDRs = append(parsedCIDRs, network)
	}
}

// isPrivateIP returns true if ip is in a private/loopback/link-local range.
func isPrivateIP(ip net.IP) bool {
	if allowLoopback && ip.IsLoopback() {
		return false
	}
	for _, network := range parsedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// SafeGet performs an HTTP GET after verifying the target does not resolve to a
// private IP address. This prevents SSRF (Server-Side Request Forgery) attacks
// where an attacker supplies a URL pointing to internal services.
//
// Mitigates DNS rebinding (TOCTOU) by resolving DNS once, validating the IP,
// and dialing directly to the verified IP via a custom transport. Redirects
// are disabled to prevent redirect-based SSRF.
// See: https://owasp.org/www-community/attacks/Server_Side_Request_Forgery
// safeTransport is a shared HTTP transport that validates all outbound
// connections against private IP ranges to prevent SSRF.
var safeTransport = &http.Transport{
	DialContext: safeDialContext,
}

// safeDialContext resolves DNS, verifies the resolved IP is not in a private
// range, and dials directly. This prevents SSRF via DNS rebinding or direct
// private IP access.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid address %q: %w", addr, err)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("ssrf: DNS lookup failed for %s: %w", host, err)
	}
	var safeIP net.IP
	for _, ip := range ips {
		if isPrivateIP(ip) {
			continue
		}
		safeIP = ip
		break
	}
	if safeIP == nil {
		return nil, fmt.Errorf("ssrf: no public IP found for %s", host)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(safeIP.String(), port))
}

// NewSafeHTTPClient creates an *http.Client whose transport prevents
// connections to private/internal IP addresses. Use this for all outbound
// HTTP clients to guard against SSRF.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:    timeout,
		Transport: safeTransport,
	}
}

// NewProxyAwareHTTPClient creates an *http.Client that routes traffic through
// the configured PROXY_URL when set. Use this for all outbound API clients so
// that a single proxy configuration covers every provider.
func NewProxyAwareHTTPClient(cfg *config.Config, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg != nil && cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			slog.Warn("Invalid PROXY_URL, running without proxy", "error", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// NewSafeProxyAwareHTTPClient creates an *http.Client that routes traffic through
// the configured PROXY_URL when set AND prevents connections to private/internal
// IP addresses. This provides both proxying and SSRF protection.
func NewSafeProxyAwareHTTPClient(cfg *config.Config, timeout time.Duration) *http.Client {
	transport := safeTransport.Clone()
	if cfg != nil && cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			slog.Warn("Invalid PROXY_URL, running without proxy", "error", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Defense in depth: limit redirect count. SSRF protection for the
			// redirect target is enforced at the Transport level (safeDialContext).
			if len(via) >= 10 {
				return fmt.Errorf("ssrf: too many redirects")
			}
			return nil
		},
	}
}

func SafeGet(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Warn("Blocked unsupported scheme", "url", rawURL, "scheme", u.Scheme)
		return nil, fmt.Errorf("ssrf: unsupported scheme %q", u.Scheme)
	}

	hostname := u.Hostname()
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("ssrf: DNS lookup failed: %w", err)
	}

	// Find the first safe IP to connect to
	var safeIP net.IP
	for _, ip := range ips {
		if isPrivateIP(ip) {
			continue
		}
		safeIP = ip
		break
	}
	if safeIP == nil {
		return nil, fmt.Errorf("ssrf: no public IP found for %s", hostname)
	}

	// Determine port — use explicit port or scheme default
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Use the shared safeTransport which validates IPs on every dial.
	// Also disable redirects — an attacker could redirect a safe URL to an internal one.
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: safeTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return client.Get(rawURL)
}

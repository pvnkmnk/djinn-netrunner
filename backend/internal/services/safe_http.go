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
	"10.0.0.0/8",      // RFC 1918
	"172.16.0.0/12",   // RFC 1918
	"192.168.0.0/16",  // RFC 1918
	"127.0.0.0/8",     // IPv4 Loopback
	"169.254.0.0/16",  // IPv4 Link-Local
	"0.0.0.0/8",       // Current network
	"::1/128",         // IPv6 Loopback
	"fc00::/7",        // IPv6 Unique Local
	"fe80::/10",       // IPv6 Link-Local
}

// parsedCIDRs is populated at init time.
var parsedCIDRs []*net.IPNet

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
	// Normalize IPv4-mapped IPv6 addresses (e.g. ::ffff:127.0.0.1) to 4-byte IPv4.
	// This prevents bypasses where an attacker uses the IPv6 representation of a private IPv4.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	for _, network := range parsedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

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
		Timeout:   timeout,
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
// the configured PROXY_URL when set AND prevents SSRF for direct connections.
//
// If a proxy is configured, SSRF protection is offloaded to the proxy, and we
// allow connecting to the proxy even if it resides on a private IP address.
func NewSafeProxyAwareHTTPClient(cfg *config.Config, timeout time.Duration) *http.Client {
	if cfg != nil && cfg.ProxyURL != "" {
		// If using a proxy, use the standard proxy-aware client.
		return NewProxyAwareHTTPClient(cfg, timeout)
	}

	// No proxy: use the safe client that validates all direct outbound dials.
	return NewSafeHTTPClient(timeout)
}

// SafeGet performs an HTTP GET after verifying the target does not resolve to a
// private IP address. This prevents SSRF (Server-Side Request Forgery) attacks
// where an attacker supplies a URL pointing to internal services.
//
// Mitigates DNS rebinding (TOCTOU) by resolving DNS once, validating the IP,
// and dialing directly to the verified IP via a custom transport.
// See: https://owasp.org/www-community/attacks/Server_Side_Request_Forgery
func SafeGet(rawURL string) (*http.Response, error) {
	client := NewSafeHTTPClient(30 * time.Second)

	// Custom CheckRedirect to ensure redirected URLs are also validated.
	// NewSafeHTTPClient uses safeTransport which validates on every Dial,
	// so even redirected URLs will be checked at dial time.
	// We allow up to 10 redirects.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	return client.Get(rawURL)
}

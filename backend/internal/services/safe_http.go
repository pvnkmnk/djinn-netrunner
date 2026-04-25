package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
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

	// Build a transport that dials directly to the verified IP, preventing
	// DNS rebinding (TOCTOU) where a second DNS lookup could return a private IP.
	dialAddr := net.JoinHostPort(safeIP.String(), port)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("tcp", dialAddr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: hostname, // SNI must match the original hostname for HTTPS
		},
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		// Disable redirects — an attacker could redirect a safe URL to an internal one
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return client.Get(rawURL)
}

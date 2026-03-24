package services

import (
	"fmt"
	"log"
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
// See: https://owasp.org/www-community/attacks/Server_Side_Request_Forgery
func SafeGet(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		log.Printf("[SSRF] Blocked unsupported scheme | url=%s scheme=%s", rawURL, u.Scheme)
		return nil, fmt.Errorf("ssrf: unsupported scheme %q", u.Scheme)
	}

	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("ssrf: DNS lookup failed: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("ssrf: blocked private IP %s for %s", ip, u.Hostname())
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Get(rawURL)
}

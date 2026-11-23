package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// nonGlobalCIDRs contains CIDR ranges that must never be dialed by outbound
// HTTP: the private/loopback/link-local ranges, plus the shared, reserved and
// documentation ranges net.IP.IsGlobalUnicast admits but no public service
// should ever answer from. The list is the auditable record of what "global"
// means here — a hand list is deliberate, so the guard fails closed on an
// address it cannot classify as global unicast.
var nonGlobalCIDRs = []string{
	// Private / loopback / link-local (RFC 1918, RFC 3927, RFC 4291).
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"fe80::/10", // IPv6 Link-Local
	"0.0.0.0/8",
	// Shared address space / CGNAT (RFC 6598). Reachable from the host's
	// network, so a DNS answer here is an SSRF vector, not a public host.
	"100.64.0.0/10",
	// Documentation / TEST-NETs (RFC 5737, RFC 3849).
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
	// IETF protocol assignments (RFC 6890) and benchmarking (RFC 2544).
	"192.0.0.0/24",
	"198.18.0.0/15",
	// Reserved (RFC 1112, RFC 6890) and multicast.
	"240.0.0.0/4",
	"224.0.0.0/4",
	// IPv6 specials: documentation (2001:db8::/32 and the RFC 9637
	// documentation prefix 3fff::/20), IETF special-purpose (Teredo, ORCHID,
	// benchmarking), 6to4, the NAT64 prefixes (well-known 64:ff9b::/96 and
	// local-use 64:ff9b:1::/48 — both translate to IPv4, so either can smuggle
	// a private address past a v6-only check), discard-only, deprecated
	// site-local, multicast, unspecified.
	"2001:db8::/32",
	"3fff::/20",
	"2001::/23",
	"2002::/16",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"fec0::/10",
	"ff00::/8",
	"::/128",
}

// parsedNonGlobalCIDRs is populated at init time.
var parsedNonGlobalCIDRs []*net.IPNet

func init() {
	for _, cidr := range nonGlobalCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
		}
		parsedNonGlobalCIDRs = append(parsedNonGlobalCIDRs, network)
	}
}

// ErrDisallowedDestination marks a URL the guard refused: an address that
// resolved into a non-global range. It is a permanent verdict about that URL, so
// callers record it terminally instead of retrying the same address.
var ErrDisallowedDestination = errors.New("ssrf: disallowed destination")

// disallowedDestinationError carries the guard's own wording while matching
// ErrDisallowedDestination, so existing messages (which tests and operators
// read) stay exactly as they were.
type disallowedDestinationError struct{ msg string }

func (e *disallowedDestinationError) Error() string { return e.msg }

func (e *disallowedDestinationError) Unwrap() error { return ErrDisallowedDestination }

// refusedDestination builds one of those refusals.
func refusedDestination(msg string) error {
	return &disallowedDestinationError{msg: msg}
}

// redirectHopLimit bounds the chain the handover walk will follow. A longer
// chain fails rather than being walked: an unbounded redirect chain is exactly
// what a guard cannot afford to follow.
const redirectHopLimit = 10

// isNonGlobalIP returns true if ip is not a global unicast address: private,
// loopback, link-local, multicast, unspecified, shared (CGNAT), reserved or
// documentation space. Only global unicast is dialable. IsGlobalUnicast alone
// is not enough — it admits CGNAT, the TEST-NETs, benchmarking, the reserved
// range and the documentation ranges — so the hand list above covers those
// explicitly.
func isNonGlobalIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 (::ffff:x.x.x.x) to 4-byte IPv4.
	// Prevents SSRF bypass using IPv6 representations of private IPv4 addresses.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if !ip.IsGlobalUnicast() {
		return true
	}
	for _, network := range parsedNonGlobalCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// safeAddressValidator wraps an http.RoundTripper to validate that the
// request URL's hostname resolves to a public IP before delegating to
// the inner transport. This prevents SSRF when requests flow through a
// proxy, where the transport-level safeDialContext only sees the proxy
// server's address.
type safeAddressValidator struct {
	next http.RoundTripper
}

func (v *safeAddressValidator) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := checkPublicHost(req.URL.Hostname()); err != nil {
		return nil, err
	}
	return v.next.RoundTrip(req)
}

// checkPublicHost resolves host and refuses it if it resolves to a non-global
// address. It is the one implementation of "is this destination allowed",
// shared by the request-path guard above and by callers that hand a URL to
// a tool which makes its own connections (see YtdlpService.DownloadAudio).
func checkPublicHost(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("ssrf: DNS lookup failed for %s: %w", host, err)
	}
	for _, ip := range ips {
		if isNonGlobalIP(ip) {
			return refusedDestination(fmt.Sprintf("ssrf: target %s resolves to non-global IP %s", host, ip.String()))
		}
	}
	return nil
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
// connections against non-global IP ranges to prevent SSRF.
var safeTransport = &http.Transport{
	DialContext: safeDialContext,
}

// safeDialContext resolves DNS, verifies the resolved IP is not in a non-global
// range, and dials directly. This prevents SSRF via DNS rebinding or direct
// non-global IP access.
//
// It tries every resolved global-unicast IP in order until one dials: DNS can answer
// an unreachable address first (notably IPv6 on a host with no IPv6 route),
// and stopping at the first answer turns that into a hard failure for a host
// that is otherwise reachable — which is exactly how the DJI-501 egress probe
// failed in CI (see DJI-682). Non-global addresses are never dialed. If no
// global-unicast IP exists the destination is refused (ErrDisallowedDestination); if
// global-unicast IPs exist but none dials, the dial errors are returned joined,
// which is a transient failure, not a refusal. When the caller's context
// carries no deadline, the whole dial sequence is bounded by
// overallDialTimeout (below) instead of granting each attempt a fresh
// timeout with no total cap.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid address %q: %w", addr, err)
	}
	// Bound the whole sequence — including DNS — before resolving: the
	// lookup is context-aware, so a stalled resolution or a caller
	// cancellation ends the call instead of holding the request past the
	// caller's deadline and the overall dial budget (Sourcery on #394).
	ctx, cancel := withOverallDialDeadline(ctx)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("ssrf: DNS lookup failed for %s: %w", host, err)
	}
	var d net.Dialer
	return dialPublicIPs(ctx, network, host, port, ips, d.DialContext)
}

// withOverallDialDeadline returns ctx bounded by the overall dial budget
// when it carries no deadline yet. A ctx that already has a deadline is
// returned unchanged with a no-op cancel, so callers can always defer it.
func withOverallDialDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, overallDialTimeout)
}

// defaultPerDialTimeout bounds one dial attempt when the caller's context
// carries no deadline. Every production caller sets a client timeout, so
// this is a backstop, not the normal path.
const defaultPerDialTimeout = 10 * time.Second

// maxDialAttempts bounds the fair-share window dialPublicIPs gives its first
// candidates. DNS rarely answers more than a handful of addresses; eight
// keeps every realistic answer set whole while bounding the pathological one,
// so a large set cannot shrink each attempt's share of the deadline to
// nothing. It is a window, not a hard cap: distinct addresses past the window
// still get overflow attempts (below), so a reachable address late in DNS
// order is tried instead of truncated away (Sourcery on #393).
const maxDialAttempts = 8

// maxOverflowAttempts bounds the overflow phase's candidate count as well as
// its time budget: with fast failures, time alone does not bound how many
// outbound dials one request makes. Eight matches the fair-share window, so
// one request dials at most sixteen distinct addresses (Sourcery on #394).
const maxOverflowAttempts = 8

// overflowPerDialTimeout bounds one overflow dial attempt — a candidate past
// the fair-share window. Small on purpose: the window already had the fair
// share of the deadline, so overflow attempts only spend time the window
// left over (fast refusals), never a fresh slice of it.
const overflowPerDialTimeout = 1 * time.Second

// overallDialTimeout bounds the whole dial sequence — fair-share window and
// overflow alike — when the caller's context carries no deadline. It equals
// the window's worst case (one backstop budget per window attempt), so the
// per-attempt backstop is unchanged and the overflow only spends what the
// window left over, instead of granting each overflow address a fresh
// timeout with no total cap (CodeRabbit on #394).
const overallDialTimeout = defaultPerDialTimeout * maxDialAttempts

// dialPublicIPs dials the first reachable global-unicast IP in ips, trying each in
// order. Sequential, not parallel: the order is deterministic, which keeps
// the guard auditable, and a dial to an unreachable address fails fast.
//
// The first maxDialAttempts candidates each get a fair share of the time
// left on the caller's deadline (or defaultPerDialTimeout when there is
// none): a blackholed address must not consume the whole request budget and
// starve the addresses behind it. Up to maxOverflowAttempts distinct
// addresses past that window get one overflow attempt each, bounded to
// overflowPerDialTimeout and to the time still left on the caller's
// deadline — so a reachable address late in DNS order is still tried when
// the window's attempts failed fast, while a blackholed window under a tight
// deadline still cannot spend time it does not have. Repeated answers count
// once, so duplicates cannot eat the budget before a distinct reachable
// address is tried. The dial function is a parameter (a net.Dialer's
// DialContext in production) so the fallback loop is testable without a
// network.
func dialPublicIPs(ctx context.Context, network, host, port string, ips []net.IP, dial func(ctx context.Context, network, address string) (net.Conn, error)) (net.Conn, error) {
	// Filter to dialable addresses first, so the per-attempt budget divides
	// over addresses that will actually be dialed.
	var targets []net.IP
	seen := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		if isNonGlobalIP(ip) {
			continue
		}
		// Normalize IPv4-mapped IPv6 (::ffff:1.1.1.1) to 4-byte IPv4 so the
		// same address in two forms dedupes to one target.
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		if _, ok := seen[ip.String()]; ok {
			continue
		}
		seen[ip.String()] = struct{}{}
		targets = append(targets, ip)
	}
	if len(targets) == 0 {
		return nil, refusedDestination(fmt.Sprintf("ssrf: no public IP found for %s", host))
	}
	window := min(len(targets), maxDialAttempts)
	var errs []error
	for i := 0; i < window; i++ {
		attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout(ctx, window-i))
		conn, err := dial(attemptCtx, network, net.JoinHostPort(targets[i].String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
	}
	// Overflow: candidates past the fair-share window, one small attempt
	// each, only while the caller's deadline still has time left. A
	// blackholed window under a tight deadline leaves nothing here, by
	// design — the overflow spends leftover time, never a fresh slice.
	// The count is capped too: with fast failures, time alone does not
	// bound how many outbound dials one request makes.
	overflow := targets[window:]
	if len(overflow) > maxOverflowAttempts {
		overflow = overflow[:maxOverflowAttempts]
	}
	for _, ip := range overflow {
		budget := overflowAttemptTimeout(ctx)
		if budget <= 0 {
			break
		}
		attemptCtx, cancel := context.WithTimeout(ctx, budget)
		conn, err := dial(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("ssrf: could not reach %s: %w", host, errors.Join(errs...))
}

// overflowAttemptTimeout is the budget for one overflow dial attempt: at most
// overflowPerDialTimeout, and never past the caller's deadline. Zero (or
// negative) means no time is left, so the overflow loop stops.
func overflowAttemptTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			return min(remaining, overflowPerDialTimeout)
		}
		return 0
	}
	return overflowPerDialTimeout
}

// perAttemptTimeout is the budget for one dial attempt: an even share of the
// time left on the caller's deadline across the attempts still to run, or
// defaultPerDialTimeout when the caller set no deadline.
func perAttemptTimeout(ctx context.Context, attemptsLeft int) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			return remaining / time.Duration(attemptsLeft)
		}
		return 0
	}
	return defaultPerDialTimeout
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

// NewSafeProxyAwareHTTPClient creates an *http.Client for calling configured
// backend services (slskd, gonic, …).
//
// Default (cfg.AllowPrivateTargets == false): SSRF protection is applied in
// all cases.
//   - Without a proxy: uses safeTransport, which validates all outbound
//     connections against non-global IP ranges at dial time (same as
//     NewSafeHTTPClient).
//   - With a proxy: validates that the target URL resolves to a public IP
//     before sending the request through the proxy. The proxy server itself
//     can be on a private network (common for corporate forward proxies).
//
// When cfg.AllowPrivateTargets is true (env ALLOW_PRIVATE_TARGETS=true), the
// private-range checks are skipped for this client. This is an explicit opt-in
// for trusted internal service meshes — docker-network peers (e.g.
// SLSKD_URL=http://netrunner-slskd:5030) and integration-test harnesses —
// where the configured target hostname is operator-controlled, not
// user-supplied. Helpers that handle user-influenced URLs (e.g. SafeGet for
// cover art) intentionally ignore this switch and remain guarded.
func NewSafeProxyAwareHTTPClient(cfg *config.Config, timeout time.Duration) *http.Client {
	if cfg != nil && cfg.AllowPrivateTargets {
		// Trusted internal mode: dial directly (optionally via proxy), no
		// private-range validation.
		if cfg.ProxyURL != "" {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			if proxyURL, err := url.Parse(cfg.ProxyURL); err == nil {
				transport.Proxy = http.ProxyURL(proxyURL)
			} else {
				slog.Warn("Invalid PROXY_URL, running without proxy", "error", err)
			}
			return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: timeout}
		}
		return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: timeout}
	}

	if cfg != nil && cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			slog.Warn("Invalid PROXY_URL, running without proxy", "error", err)
			return NewSafeHTTPClient(timeout)
		}

		// Clone default transport (safeDialContext cannot see the target through a proxy,
		// so we validate at the RoundTrip level instead).
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxyURL)

		return &http.Client{
			Transport: &safeAddressValidator{next: transport},
			Timeout:   timeout,
		}
	}

	return NewSafeHTTPClient(timeout)
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
		if isNonGlobalIP(ip) {
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

// hopBoundCheck is the hop bound the handover walk uses. It is separate from
// the client so the bound is testable without a network.
func hopBoundCheck(req *http.Request, via []*http.Request) error {
	if len(via) >= redirectHopLimit {
		return fmt.Errorf("redirect chain longer than %d hops", redirectHopLimit)
	}
	return nil
}

// newRedirectResolvingClient builds the client used before a URL is handed to a
// tool that makes its own connections: the repository's safe transport, so every
// hop is dialed through safeDialContext, plus the hop bound above.
func newRedirectResolvingClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     safeTransport,
		CheckRedirect: hopBoundCheck,
	}
}

// resolveRedirectTarget walks rawURL's redirect chain and returns the final URL,
// so a downloader that follows redirects on its own can be handed a destination
// that has already been checked. Every hop is dialed through safeDialContext —
// the repository's one guard for outbound connections — so a hop to a private
// address fails the walk instead of being followed, and the refusal is
// ErrDisallowedDestination rather than an ordinary transport error.
//
// The body is never read: this asks for the destination, not the content, and
// the downloader still does the downloading.
func resolveRedirectTarget(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request for %s: %w", rawURL, err)
	}
	// Ask for no bytes: the chain is all this needs, and a media file behind
	// these URLs can be large.
	req.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(req)
	if err != nil {
		// Either the guard refused a hop or the chain could not be walked at
		// all; the caller decides which of those is permanent.
		return "", err
	}
	defer resp.Body.Close() // never read: the downloader fetches the content

	final := resp.Request.URL
	if final == nil {
		return "", fmt.Errorf("resolve %s: no final URL", rawURL)
	}
	// The chain's last hop was dialed, not necessarily re-resolved; check the
	// name it ended on for the same reason the first host is checked.
	if err := checkPublicHost(final.Hostname()); err != nil {
		return "", fmt.Errorf("redirect target %s: %w", final.Host, err)
	}
	return final.String(), nil
}

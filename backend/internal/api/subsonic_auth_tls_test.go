package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"
)

const (
	subsonicTLSUser     = "tls-auth@example.com"
	subsonicTLSPassword = "account-password-secret"
)

type subsonicHTTPTestServer struct {
	app    *fiber.App
	client *http.Client
	base   string
}

func startSubsonicHTTPTestServer(t *testing.T, handler *SubsonicHandler, tlsEnabled bool, logOutput io.Writer) *subsonicHTTPTestServer {
	t.Helper()

	// Match the server's broad Fiber proxy trust for loopback. The Subsonic
	// middleware must still consult its own explicit proxy allowlist.
	app := fiber.New(fiber.Config{
		TrustProxy: true,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Proxies: []string{"127.0.0.0/8"},
		},
	})
	if logOutput != nil {
		app.Use(logger.New(logger.Config{Stream: logOutput, DisableColors: true}))
	}
	app.Get("/rest/ping.view", handler.AuthMiddleware, handler.Ping)
	app.Post("/rest/ping.view", handler.AuthMiddleware, handler.Ping)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + listener.Addr().String()
	transport := &http.Transport{}
	if tlsEnabled {
		cert, roots := makeSubsonicTestCertificate(t)
		listener = tls.NewListener(listener, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		base = "https://" + listener.Addr().String()
	}

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- app.Listener(listener)
	}()
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(time.Second); err != nil {
			t.Errorf("Fiber test server shutdown failed: %v", err)
		}
		_ = listener.Close()
		transport.CloseIdleConnections()
		select {
		case err := <-serveResult:
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("Fiber test listener returned unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Fiber test listener did not stop")
		}
	})

	return &subsonicHTTPTestServer{
		app: app,
		client: &http.Client{
			Transport:     transport,
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		base: base,
	}
}

func makeSubsonicTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return certificate, roots
}

func subsonicHTTPAuthRequest(t *testing.T, server *subsonicHTTPTestServer, method string, values url.Values, forwardedProto ...string) (int, string) {
	t.Helper()
	endpoint := server.base + "/rest/ping.view"
	var body io.Reader
	if method == http.MethodGet {
		endpoint += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequest(method, endpoint, body)
	require.NoError(t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, value := range forwardedProto {
		req.Header.Add("X-Forwarded-Proto", value)
	}
	resp, err := server.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(responseBody)
}

func subsonicTestToken(sharedPassword, salt string) string {
	passwordHash := md5Hex(sharedPassword)
	return md5Hex(passwordHash + salt)
}

func md5Hex(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}

func captureSubsonicAuthLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

func TestSubsonicLegacyPasswordRequiresVerifiedHTTPS(t *testing.T) {
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)
	var logs bytes.Buffer
	server := startSubsonicHTTPTestServer(t, handler, false, &logs)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
		status, body := subsonicHTTPAuthRequest(t, server, method, values)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, body, `"status":"failed"`)
		require.Contains(t, body, "Password authentication requires HTTPS")
		require.NotContains(t, body, subsonicTLSPassword)
	}

	// Fiber globally trusts loopback for forwarded headers, but that does not
	// make this peer trusted for Subsonic's external-scheme decision.
	values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
	status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values, "https")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"failed"`)
	require.NotContains(t, body, subsonicTLSPassword)

	// The application logger's default format records the path, not query or
	// form credentials. No auth diagnostic logs the submitted values either.
	require.NotContains(t, logs.String(), subsonicTLSPassword)
	require.NotContains(t, logs.String(), subsonicTLSUser)
}

func TestSubsonicAuthenticationDoesNotLogCredentialValues(t *testing.T) {
	const sharedPassword = "server-shared-secret"
	const salt = "query-salt-secret"
	const invalidToken = "00000000000000000000000000000000"
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	var dbLogs bytes.Buffer
	db.Config.Logger = gormlogger.New(log.New(&dbLogs, "", 0), gormlogger.Config{
		LogLevel: gormlogger.Info,
		Colorful: false,
	})
	cfg := &config.Config{}
	cfg.Subsonic.Password = sharedPassword
	handler := NewSubsonicHandler(db, cfg)
	var appLogs bytes.Buffer
	server := startSubsonicHTTPTestServer(t, handler, true, &appLogs)

	passwordValues := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
	status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, passwordValues)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"ok"`)

	tokenValues := url.Values{"u": {subsonicTLSUser}, "t": {invalidToken}, "s": {salt}, "f": {"json"}}
	status, body = subsonicHTTPAuthRequest(t, server, http.MethodGet, tokenValues)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"failed"`)

	for _, credential := range []string{subsonicTLSUser, subsonicTLSPassword, sharedPassword, invalidToken, salt} {
		require.NotContains(t, dbLogs.String(), credential)
		require.NotContains(t, appLogs.String(), credential)
	}
}

func TestSubsonicUserLookupFailuresLogFixedReasonsWithoutValues(t *testing.T) {
	cases := []struct {
		name   string
		dbDown bool
		reason string
	}{
		{name: "unknown account", reason: "user_not_found"},
		{name: "database unavailable", dbDown: true, reason: "user_lookup_failed"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSubsonicTestDB(t)
			if tt.dbDown {
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			}

			var dbLogs bytes.Buffer
			db.Config.Logger = gormlogger.New(log.New(&dbLogs, "", 0), gormlogger.Config{
				LogLevel: gormlogger.Info,
				Colorful: false,
			})
			slogLogs := captureSubsonicAuthLogs(t)
			var appLogs bytes.Buffer
			handler := NewSubsonicHandler(db, &config.Config{})
			server := startSubsonicHTTPTestServer(t, handler, true, &appLogs)

			username := "lookup-user-secret@example.invalid"
			password := "lookup-password-secret"
			clientMarker := "query-value-secret"
			values := url.Values{
				"u": {username},
				"p": {password},
				"c": {clientMarker},
				"f": {"json"},
			}
			status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values)
			require.Equal(t, http.StatusOK, status)
			require.Contains(t, body, `"status":"failed"`)

			structured := slogLogs.String()
			require.Contains(t, structured, "reason="+tt.reason)
			if tt.dbDown {
				require.Contains(t, structured, "level=ERROR")
				require.NotContains(t, structured, "reason=user_not_found")
				require.NotContains(t, structured, "database is closed")
			} else {
				require.Contains(t, structured, "level=WARN")
				require.NotContains(t, structured, "reason=user_lookup_failed")
			}

			allLogs := structured + dbLogs.String() + appLogs.String()
			for _, value := range []string{username, password, clientMarker} {
				require.NotContains(t, allLogs, value)
			}
		})
	}
}

func TestSubsonicLegacyPasswordAcceptsDirectTLS(t *testing.T) {
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)
	var logs bytes.Buffer
	server := startSubsonicHTTPTestServer(t, handler, true, &logs)

	values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
	status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"ok"`)
	require.NotContains(t, logs.String(), subsonicTLSUser)
	require.NotContains(t, logs.String(), subsonicTLSPassword)
}

func TestSubsonicTrustedProxyTLSOriginUsesExternalForwardedProto(t *testing.T) {
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{SubsonicTrustedProxyCIDRs: "127.0.0.1/32"}
	handler := NewSubsonicHandler(db, cfg)
	server := startSubsonicHTTPTestServer(t, handler, true, nil)

	cases := []struct {
		name         string
		forwarded    []string
		wantAccepted bool
	}{
		{name: "external HTTP rejected despite TLS origin hop", forwarded: []string{"http"}},
		{name: "external HTTPS accepted", forwarded: []string{"https"}, wantAccepted: true},
		{name: "duplicate values rejected", forwarded: []string{"https", "https"}},
		{name: "comma-joined values rejected", forwarded: []string{"https, https"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
			status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values, tt.forwarded...)
			require.Equal(t, http.StatusOK, status)
			if tt.wantAccepted {
				require.Contains(t, body, `"status":"ok"`)
			} else {
				require.Contains(t, body, `"status":"failed"`)
				require.Contains(t, body, "Password authentication requires HTTPS")
			}
		})
	}
}

func TestSubsonicLegacyPasswordAcceptsExplicitTrustedProxyHTTPS(t *testing.T) {
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{SubsonicTrustedProxyCIDRs: "127.0.0.1/32"}
	handler := NewSubsonicHandler(db, cfg)
	server := startSubsonicHTTPTestServer(t, handler, false, nil)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
		status, body := subsonicHTTPAuthRequest(t, server, method, values, "https")
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, body, `"status":"ok"`)
	}

	// Even an allowlisted proxy must provide exactly one unambiguous scheme.
	for _, headers := range [][]string{{"https", "https"}, {"https, https"}} {
		values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
		status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values, headers...)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, body, `"status":"failed"`)
	}
}

func TestSubsonicLegacyPasswordAcceptsIPv4MappedTrustedProxyCIDR(t *testing.T) {
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{SubsonicTrustedProxyCIDRs: "::ffff:127.0.0.1/128"}
	handler := NewSubsonicHandler(db, cfg)
	server := startSubsonicHTTPTestServer(t, handler, false, nil)

	values := url.Values{"u": {subsonicTLSUser}, "p": {subsonicTLSPassword}, "f": {"json"}}
	status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values, "https")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"ok"`)
}

func TestParseSubsonicTrustedProxyCIDRsRejectsMappedPrefixBroaderThanIPv4(t *testing.T) {
	require.Empty(t, parseSubsonicTrustedProxyCIDRs("::ffff:127.0.0.1/95"))
}

func TestSubsonicTokenAuthRemainsAvailableOverCleartext(t *testing.T) {
	const sharedPassword = "shared-secret-123"
	const salt = "cleartext-test-salt"
	db := setupSubsonicTestDB(t)
	createTestUserForSubsonic(t, db, subsonicTLSUser, subsonicTLSPassword)
	cfg := &config.Config{}
	cfg.Subsonic.Password = sharedPassword
	handler := NewSubsonicHandler(db, cfg)
	var logs bytes.Buffer
	server := startSubsonicHTTPTestServer(t, handler, false, &logs)

	values := url.Values{
		"u": {subsonicTLSUser},
		"t": {subsonicTestToken(sharedPassword, salt)},
		"s": {salt},
		"f": {"json"},
	}
	status, body := subsonicHTTPAuthRequest(t, server, http.MethodGet, values)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, `"status":"ok"`)
	require.NotContains(t, logs.String(), subsonicTLSUser)
	require.NotContains(t, logs.String(), values.Get("t"))
	require.NotContains(t, logs.String(), salt)
	require.NotContains(t, logs.String(), sharedPassword)
}

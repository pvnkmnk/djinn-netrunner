//go:build integration

package integration

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// NR03 asks for the protocol credential matrix to be proven against a running
// server, not only against an in-process handler. These tests use the same
// request shapes a third-party client sends and never inject locals.

const subsonicTestSalt = "integration-salt-1"

func subsonicToken(sharedPassword string) string {
	md5Password := md5.Sum([]byte(sharedPassword))
	sum := md5.Sum([]byte(hex.EncodeToString(md5Password[:]) + subsonicTestSalt))
	return hex.EncodeToString(sum[:])
}

// subsonicDo issues GET (query) or POST (form) against a /rest endpoint and
// returns the XML body. Subsonic protocol failures are HTTP 200 envelopes;
// redirects are not followed, and unexpected non-200 statuses fail (404 skips
// when the API is disabled).
func subsonicDo(t *testing.T, baseURL, endpoint string, values url.Values, post bool) string {
	t.Helper()
	var (
		resp *http.Response
		err  error
	)
	method := http.MethodGet
	requestURL := baseURL + endpoint + "?" + values.Encode()
	var requestBody io.Reader
	if post {
		method = http.MethodPost
		requestURL = baseURL + endpoint
		requestBody = strings.NewReader(values.Encode())
	}
	request, requestErr := http.NewRequest(method, requestURL, requestBody)
	if requestErr != nil {
		t.Fatalf("%s request construction failed: %v", endpoint, requestErr)
	}
	if post {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err = newSubsonicSmokeHTTPClient().Do(request)
	if err != nil {
		t.Fatalf("%s request failed: %v", endpoint, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("%s body read failed: %v", endpoint, readErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		t.Skipf("Subsonic API is not enabled on %s (404 on %s)", baseURL, endpoint)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s expected 200 envelope, got %d: %s", endpoint, resp.StatusCode, string(body))
	}
	return string(body)
}

// newSubsonicSmokeHTTPClient keeps HTTP rejection assertions on the original
// request: the default redirect policy could silently retry p= over HTTPS.
// The default transport verifies TLS certificates for HTTPS success cases.
func newSubsonicSmokeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestSubsonicSmokeClientDoesNotFollowRedirects(t *testing.T) {
	finalHit := make(chan struct{}, 1)
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		finalHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirect.Close()

	response, err := newSubsonicSmokeHTTPClient().Get(redirect.URL)
	if err != nil {
		t.Fatalf("redirect probe failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("expected original 302 response, got %d", response.StatusCode)
	}
	select {
	case <-finalHit:
		t.Fatal("client followed the redirect")
	default:
	}
}

func subsonicLogin(t *testing.T, baseURL string) (email, password string) {
	t.Helper()
	email = fmt.Sprintf("smoke-nr03-subsonic-%d@test.com", len(t.Name()))
	password = "TestPass123!subsonic"

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := newSubsonicSmokeHTTPClient()
	client.Jar = jar
	body := `{"email":"` + email + `","password":"` + password + `"}`
	for _, path := range []string{"/api/auth/register", "/api/auth/login"} {
		req, reqErr := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
		if reqErr != nil {
			t.Fatalf("%s: %v", path, reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("%s: %v", path, doErr)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s expected 200/201, got %d", path, resp.StatusCode)
		}
	}
	return email, password
}

func TestSmoke_Subsonic_Auth_CredentialModes(t *testing.T) {
	skipIfShort(t)
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	email, password := subsonicLogin(t, baseURL)

	t.Run("legacy password mode requires HTTPS", func(t *testing.T) {
		values := url.Values{"u": {email}, "p": {password}}
		for _, post := range []bool{false, true} {
			body := subsonicDo(t, baseURL, "/rest/ping.view", values, post)
			if strings.HasPrefix(strings.ToLower(baseURL), "http://") {
				if !strings.Contains(body, `status="failed"`) || !strings.Contains(body, "Password authentication requires HTTPS") {
					t.Fatalf("post=%v: expected cleartext p= rejection, got %s", post, body)
				}
			} else if strings.HasPrefix(strings.ToLower(baseURL), "https://") && !strings.Contains(body, `status="ok"`) {
				t.Fatalf("post=%v: expected verified HTTPS password auth, got %s", post, body)
			} else if !strings.HasPrefix(strings.ToLower(baseURL), "https://") {
				t.Fatalf("INTEGRATION_BASE_URL must use http or verified https, got %q", baseURL)
			}
			if strings.Contains(body, password) {
				t.Fatalf("post=%v: response leaked the account password", post)
			}
		}
	})

	t.Run("conflicting credential modes fail closed", func(t *testing.T) {
		values := url.Values{
			"u": {email},
			"p": {password},
			"t": {"0123456789abcdef0123456789abcdef"},
			"s": {subsonicTestSalt},
		}
		for _, post := range []bool{false, true} {
			body := subsonicDo(t, baseURL, "/rest/ping.view", values, post)
			if !strings.Contains(body, `status="failed"`) || !strings.Contains(body, `code="40"`) {
				t.Fatalf("post=%v: expected code 40 failure, got %s", post, body)
			}
			if strings.Contains(body, password) {
				t.Fatalf("post=%v: response leaked the account password", post)
			}
		}
	})

	t.Run("incomplete token mode fails", func(t *testing.T) {
		body := subsonicDo(t, baseURL, "/rest/ping.view", url.Values{"u": {email}, "t": {"deadbeef"}}, false)
		if !strings.Contains(body, `code="40"`) {
			t.Fatalf("expected code 40 for token without salt, got %s", body)
		}
	})

	t.Run("cleartext password is rejected before username validation", func(t *testing.T) {
		body := subsonicDo(t, baseURL, "/rest/ping.view", url.Values{"p": {password}}, false)
		if strings.HasPrefix(strings.ToLower(baseURL), "http://") {
			if !strings.Contains(body, "Password authentication requires HTTPS") {
				t.Fatalf("expected transport rejection before user validation, got %s", body)
			}
		} else if !strings.Contains(body, "Missing parameter: u") {
			t.Fatalf("expected the missing-u envelope over verified HTTPS, got %s", body)
		}
	})

	t.Run("wrong password fails without leaking it", func(t *testing.T) {
		body := subsonicDo(t, baseURL, "/rest/ping.view", url.Values{"u": {email}, "p": {"wrong-password"}}, false)
		if !strings.Contains(body, `code="40"`) {
			t.Fatalf("expected code 40 for a wrong password, got %s", body)
		}
		if strings.Contains(body, "wrong-password") {
			t.Fatal("response echoed the submitted password")
		}
	})
}

// Token mode can only succeed when the server has SUBSONIC_PASSWORD configured.
// The shared secret is not discoverable from the outside, so the caller opts in
// explicitly; skipping is reported rather than silently passing.
func TestSmoke_Subsonic_Auth_TokenModeWhenConfigured(t *testing.T) {
	skipIfShort(t)
	shared := GetEnvOrDefault("INTEGRATION_SUBSONIC_PASSWORD", "")
	if shared == "" {
		t.Skip("set INTEGRATION_SUBSONIC_PASSWORD to a running server's SUBSONIC_PASSWORD to exercise token mode")
	}
	baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
	email, _ := subsonicLogin(t, baseURL)

	values := url.Values{"u": {email}, "t": {subsonicToken(shared)}, "s": {subsonicTestSalt}}
	for _, post := range []bool{false, true} {
		body := subsonicDo(t, baseURL, "/rest/ping.view", values, post)
		if !strings.Contains(body, `status="ok"`) {
			t.Fatalf("post=%v: expected token auth to succeed, got %s", post, body)
		}
	}
}

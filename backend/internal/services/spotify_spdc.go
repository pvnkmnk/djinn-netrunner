package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SpDcTokenResponse represents the JSON response from Spotify's /api/token endpoint.
type SpDcTokenResponse struct {
	AccessToken                      string `json:"accessToken"`
	AccessTokenExpirationTimestampMs int64  `json:"accessTokenExpirationTimestampMs"`
	IsAnonymous                      bool   `json:"isAnonymous"`
	ClientID                         string `json:"clientId"`
	Username                         string `json:"username,omitempty"`
}

// ClientTokenResponse represents the response from Spotify's client token endpoint.
type ClientTokenResponse struct {
	ResponseType string `json:"response_type"`
	GrantedToken struct {
		Token               string `json:"token"`
		ExpiresAfterSeconds int    `json:"expires_after_seconds"`
	} `json:"granted_token"`
}

// spDcUserSession holds per-user sp_dc-derived auth state.
type spDcUserSession struct {
	spDcCookie    string
	accessToken   string
	tokenExpiry   time.Time
	clientID      string
	username      string
	clientToken   string
	spTDeviceID   string
}

// SpDcAuth manages Spotify authentication via the sp_dc cookie approach.
// Safe for concurrent use across multiple users — per-user sp_dc state is
// isolated in user-keyed sessions, while shared resources (client credentials,
// client version, httpClient) are protected by a single mutex.
//
// Two-pronged strategy (ported from Stash Android app):
//
// Prong 1 — Client Credentials (public data):
// Uses well-known client credentials to access api.spotify.com/v1 for public
// playlists and track metadata. No user login required, no 429 blocks.
//
// Prong 2 — sp_dc token (user-specific data):
// Exchanges the user's sp_dc browser cookie for a web-player access token,
// then uses it with the GraphQL Partner API for user-specific data like
// library playlists, Liked Songs, and algorithmic playlists.
type SpDcAuth struct {
	httpClient *http.Client

	mu sync.RWMutex

	// Prong 1: client credentials (shared across all users)
	ccToken       string
	ccTokenExpiry time.Time

	// Prong 2: per-user sp_dc sessions keyed by user ID
	userSessions  map[uint64]*spDcUserSession
	activeUserID  uint64 // set before each sp_dc operation

	// Shared state
	clientVersion string
}

const (
	spotifyTokenEndpoint       = "https://open.spotify.com/api/token"
	spotifyClientTokenEndpoint = "https://clienttoken.spotify.com/v1/clienttoken"
	spotifyGraphQLEndpoint     = "https://api-partner.spotify.com/pathfinder/v1/query"
	spotifyWebAPIBase          = "https://api.spotify.com/v1"
	spotifyAccountsTokenURL    = "https://accounts.spotify.com/api/token"
	spotifyOpenURL             = "https://open.spotify.com"

	// SpotDL's well-known Spotify OAuth client credentials.
	// Used for client_credentials flow — public data only, no 429 blocks.
	spotdlClientID     = "5f573c9620494bae87890c0f08a60293"   // gitleaks:allow — well-known SpotDL OAuth client ID
	spotdlClientSecret = "212476d9b0f3472eaa762d90b19b0ba8" // gitleaks:allow — well-known SpotDL OAuth client secret

	// GraphQL persisted query hashes (scraped from Spotify web player JS bundles).
	hashLibraryV3         = "973e511ca44261fda7eebac8b653155e7caee3675abb4fb110cc1b8c78b091c3"
	hashFetchPlaylist     = "32b05e92e438438408674f95d0fdad8082865dc32acd55bd97f5113b8579092b"
	hashFetchLibraryTracks = "087278b20b743578a6262c2b0b4bcd20d879c503cc359a2285baf083ef944240"
	hashHome              = "23e37f2e58d82d567f27080101d36609009d8c3676457b1086cb0acc55b72a5d"

	clientVersionFallback = "1.2.87.311.g2db0c2c4"

	browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
)

var clientVersionRegex = regexp.MustCompile(`"clientVersion"\s*:\s*"([^"]+)"`)

// NewSpDcAuth creates a new SpDcAuth instance.
func NewSpDcAuth(httpClient *http.Client) *SpDcAuth {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &SpDcAuth{
		httpClient:   httpClient,
		userSessions: make(map[uint64]*spDcUserSession),
	}
}

// getOrCreateSession returns the session for the active user, creating one if needed.
// Must be called with mu held (read or write).
func (a *SpDcAuth) getSession() *spDcUserSession {
	sess, ok := a.userSessions[a.activeUserID]
	if !ok {
		return nil
	}
	return sess
}

// getOrCreateSessionWrite returns the session for the active user, creating if needed.
// Must be called with mu write-locked.
func (a *SpDcAuth) getOrCreateSession() *spDcUserSession {
	sess, ok := a.userSessions[a.activeUserID]
	if !ok {
		sess = &spDcUserSession{}
		a.userSessions[a.activeUserID] = sess
	}
	return sess
}

// SetActiveUser sets the active user ID for subsequent sp_dc operations.
// Must be called before any Prong 2 method to ensure per-user session isolation.
func (a *SpDcAuth) SetActiveUser(userID uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeUserID = userID
}

// SetSpDcCookie configures the sp_dc cookie for the active user.
func (a *SpDcAuth) SetSpDcCookie(cookie string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeUserID == 0 {
		slog.Warn("SetSpDcCookie called without active user")
		return
	}
	sess := a.getOrCreateSession()
	if sess.spDcCookie != cookie {
		sess.spDcCookie = cookie
		// Invalidate cached tokens when cookie changes
		sess.accessToken = ""
		sess.tokenExpiry = time.Time{}
		sess.clientToken = ""
	}
}

// HasSpDcCookie returns true if the active user has an sp_dc cookie configured.
func (a *SpDcAuth) HasSpDcCookie() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	sess := a.getSession()
	return sess != nil && sess.spDcCookie != ""
}

// ValidateSpDcCookie exchanges the sp_dc cookie for an access token to verify it's valid.
// Returns the resolved username on success. This method is stateless — it does not
// modify any cached session.
func (a *SpDcAuth) ValidateSpDcCookie(cookie string) (string, error) {
	resp, err := a.exchangeSpDcToken(cookie)
	if err != nil {
		return "", fmt.Errorf("sp_dc validation failed: %w", err)
	}
	if resp.IsAnonymous {
		return "", fmt.Errorf("sp_dc cookie is invalid or expired (anonymous token)")
	}
	return resp.Username, nil
}

// GetClientCredentialsToken returns a valid client_credentials token for the public Web API.
func (a *SpDcAuth) GetClientCredentialsToken() (string, error) {
	a.mu.RLock()
	if a.ccToken != "" && time.Now().Before(a.ccTokenExpiry) {
		token := a.ccToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock
	if a.ccToken != "" && time.Now().Before(a.ccTokenExpiry) {
		return a.ccToken, nil
	}

	token, err := a.fetchClientCredentialsToken()
	if err != nil {
		return "", err
	}
	a.ccToken = token
	a.ccTokenExpiry = time.Now().Add(55 * time.Minute) // 1 hour minus safety margin
	return token, nil
}

// GetSpDcAccessToken returns a valid sp_dc-derived access token for the active user.
func (a *SpDcAuth) GetSpDcAccessToken() (string, error) {
	a.mu.RLock()
	sess := a.getSession()
	if sess != nil && sess.accessToken != "" && time.Now().Before(sess.tokenExpiry) {
		token := sess.accessToken
		a.mu.RUnlock()
		return token, nil
	}
	var cookie string
	if sess != nil {
		cookie = sess.spDcCookie
	}
	a.mu.RUnlock()

	if cookie == "" {
		return "", fmt.Errorf("no sp_dc cookie configured")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	sess = a.getOrCreateSession()
	// Double-check after acquiring write lock
	if sess.accessToken != "" && time.Now().Before(sess.tokenExpiry) {
		return sess.accessToken, nil
	}

	resp, err := a.exchangeSpDcToken(cookie)
	if err != nil {
		return "", err
	}
	if resp.IsAnonymous {
		return "", fmt.Errorf("sp_dc cookie is invalid or expired")
	}

	sess.accessToken = resp.AccessToken
	sess.tokenExpiry = time.UnixMilli(resp.AccessTokenExpirationTimestampMs).Add(-60 * time.Second)
	sess.clientID = resp.ClientID
	sess.username = resp.Username
	return sess.accessToken, nil
}

// GetClientToken returns a valid client token for the GraphQL Partner API for the active user.
func (a *SpDcAuth) GetClientToken() (string, error) {
	a.mu.RLock()
	sess := a.getSession()
	if sess != nil && sess.clientToken != "" {
		token := sess.clientToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	// Need access token first to get clientID
	if _, err := a.GetSpDcAccessToken(); err != nil {
		return "", fmt.Errorf("cannot get client token without access token: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	sess = a.getOrCreateSession()
	// Double-check
	if sess.clientToken != "" {
		return sess.clientToken, nil
	}

	version := a.getClientVersionLocked()
	token, err := a.fetchClientToken(sess.clientID, version)
	if err != nil {
		return "", err
	}
	sess.clientToken = token
	return token, nil
}

// InvalidateTokens clears cached tokens for the active user, forcing re-acquisition.
func (a *SpDcAuth) InvalidateTokens() {
	a.mu.Lock()
	defer a.mu.Unlock()
	sess := a.getSession()
	if sess != nil {
		sess.accessToken = ""
		sess.tokenExpiry = time.Time{}
		sess.clientToken = ""
	}
}

// GetUsername returns the cached Spotify username for the active user.
func (a *SpDcAuth) GetUsername() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	sess := a.getSession()
	if sess == nil {
		return ""
	}
	return sess.username
}

// exchangeSpDcToken exchanges an sp_dc cookie for an access token.
func (a *SpDcAuth) exchangeSpDcToken(cookie string) (*SpDcTokenResponse, error) {
	serverTime := time.Now().Unix()
	totp := generateSpotifyTOTP(serverTime)

	tokenURL := fmt.Sprintf("%s?reason=transport&productType=web-player&totp=%s&totpServer=%s&totpVer=%s",
		spotifyTokenEndpoint, totp, totp, spotifyTOTPConfig.version)

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "sp_dc="+cookie)
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("App-Platform", "WebPlayer")
	req.Header.Set("Referer", spotifyOpenURL+"/")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	// Capture sp_t cookie for client token requests (stored on active user's session)
	for _, c := range resp.Cookies() {
		if c.Name == "sp_t" && c.Value != "" {
			a.mu.Lock()
			sess := a.getOrCreateSession()
			sess.spTDeviceID = c.Value
			a.mu.Unlock()
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	var tokenResp SpDcTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	slog.Debug("sp_dc token exchange",
		"anonymous", tokenResp.IsAnonymous,
		"hasUsername", tokenResp.Username != "",
		"hasClientID", tokenResp.ClientID != "")

	return &tokenResp, nil
}

// fetchClientCredentialsToken acquires a token via the client_credentials OAuth2 flow.
func (a *SpDcAuth) fetchClientCredentialsToken() (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", spotifyAccountsTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(spotdlClientID, spotdlClientSecret)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("client credentials request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("client credentials endpoint returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse client credentials response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token in client credentials response")
	}
	return result.AccessToken, nil
}

// fetchClientToken acquires a client token from Spotify's clienttoken endpoint.
func (a *SpDcAuth) fetchClientToken(clientID, clientVersion string) (string, error) {
	deviceID := "unknown"
	if sess := a.getSession(); sess != nil && sess.spTDeviceID != "" {
		deviceID = sess.spTDeviceID
	}

	payload := map[string]interface{}{
		"client_data": map[string]interface{}{
			"client_version": clientVersion,
			"client_id":      clientID,
			"js_sdk_data": map[string]interface{}{
				"device_brand": "unknown",
				"device_model": "unknown",
				"os":           "windows",
				"os_version":   "NT 10.0",
				"device_id":    deviceID,
				"device_type":  "computer",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", spotifyClientTokenEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("client token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("client token endpoint returned HTTP %d", resp.StatusCode)
	}

	var result ClientTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse client token response: %w", err)
	}

	if result.ResponseType != "RESPONSE_GRANTED_TOKEN_RESPONSE" {
		return "", fmt.Errorf("unexpected client token response type: %s", result.ResponseType)
	}

	return result.GrantedToken.Token, nil
}

// getClientVersionLocked scrapes the current Spotify web player version. Must be called with mu held.
func (a *SpDcAuth) getClientVersionLocked() string {
	if a.clientVersion != "" {
		return a.clientVersion
	}

	req, err := http.NewRequest("GET", spotifyOpenURL, nil)
	if err != nil {
		return clientVersionFallback
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return clientVersionFallback
	}
	defer resp.Body.Close()

	// Capture sp_t from main page too (on the active user's session)
	for _, c := range resp.Cookies() {
		if c.Name == "sp_t" && c.Value != "" {
			if sess := a.getSession(); sess != nil && sess.spTDeviceID == "" {
				sess.spTDeviceID = c.Value
			}
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return clientVersionFallback
	}

	match := clientVersionRegex.FindSubmatch(body)
	if match != nil && len(match) > 1 {
		version := string(match[1])
		slog.Debug("scraped Spotify client version", "version", version)
		a.clientVersion = version
		return version
	}

	slog.Warn("could not scrape Spotify client version, using fallback")
	return clientVersionFallback
}

// ExecuteGraphQL executes a persisted query against the GraphQL Partner API.
func (a *SpDcAuth) ExecuteGraphQL(operationName, variables, hash string) (json.RawMessage, error) {
	accessToken, err := a.GetSpDcAccessToken()
	if err != nil {
		return nil, fmt.Errorf("no access token: %w", err)
	}

	clientToken, err := a.GetClientToken()
	if err != nil {
		return nil, fmt.Errorf("no client token: %w", err)
	}

	result, err := a.executeGraphQLWithTokens(operationName, variables, hash, accessToken, clientToken)
	if err != nil {
		// On 401, try refreshing tokens and retrying once
		a.InvalidateTokens()
		accessToken2, err2 := a.GetSpDcAccessToken()
		if err2 != nil {
			return nil, err // return original error
		}
		clientToken2, err2 := a.GetClientToken()
		if err2 != nil {
			return nil, err
		}
		return a.executeGraphQLWithTokens(operationName, variables, hash, accessToken2, clientToken2)
	}
	return result, nil
}

func (a *SpDcAuth) executeGraphQLWithTokens(operationName, variables, hash, accessToken, clientToken string) (json.RawMessage, error) {
	extensions := fmt.Sprintf(`{"persistedQuery":{"version":1,"sha256Hash":"%s"}}`, hash)

	reqURL := fmt.Sprintf("%s?operationName=%s&variables=%s&extensions=%s",
		spotifyGraphQLEndpoint,
		url.QueryEscape(operationName),
		url.QueryEscape(variables),
		url.QueryEscape(extensions))

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	a.mu.RLock()
	version := a.clientVersion
	a.mu.RUnlock()
	if version == "" {
		version = clientVersionFallback
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Token", clientToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("App-Platform", "WebPlayer")
	req.Header.Set("Spotify-App-Version", version)
	req.Header.Set("Origin", spotifyOpenURL)
	req.Header.Set("Referer", spotifyOpenURL+"/")
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GraphQL %s returned HTTP %d", operationName, resp.StatusCode)
	}

	return json.RawMessage(body), nil
}

// FetchPlaylistTracksViaWebAPI fetches playlist tracks using the public Web API (Prong 1).
func (a *SpDcAuth) FetchPlaylistTracksViaWebAPI(playlistID string) ([]map[string]string, error) {
	token, err := a.GetClientCredentialsToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get client credentials token: %w", err)
	}

	var allTracks []map[string]string
	nextURL := fmt.Sprintf("%s/playlists/%s/tracks?limit=100&fields=%s",
		spotifyWebAPIBase,
		playlistID,
		url.QueryEscape("items(track(id,name,artists(name),album(name,images(url)))),next,total"))

	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Web API request failed: %w", err)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			// Refresh token and retry
			a.mu.Lock()
			a.ccToken = ""
			a.mu.Unlock()
			token, err = a.GetClientCredentialsToken()
			if err != nil {
				return nil, err
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("Web API returned HTTP %d for playlist %s", resp.StatusCode, playlistID)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		var page struct {
			Items []struct {
				Track *struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Artists []struct {
						Name string `json:"name"`
					} `json:"artists"`
					Album struct {
						Name   string `json:"name"`
						Images []struct {
							URL string `json:"url"`
						} `json:"images"`
					} `json:"album"`
				} `json:"track"`
			} `json:"items"`
			Next  *string `json:"next"`
			Total int     `json:"total"`
		}

		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse Web API response: %w", err)
		}

		for _, item := range page.Items {
			if item.Track == nil {
				continue
			}
			t := item.Track
			artistName := ""
			if len(t.Artists) > 0 {
				artistName = t.Artists[0].Name
			}
			coverURL := ""
			if len(t.Album.Images) > 0 {
				coverURL = t.Album.Images[0].URL
			}
			allTracks = append(allTracks, map[string]string{
				"id":            t.ID,
				"artist":        artistName,
				"title":         t.Name,
				"album":         t.Album.Name,
				"cover_art_url": coverURL,
			})
		}

		if page.Next != nil {
			nextURL = *page.Next
		} else {
			nextURL = ""
		}
	}

	return allTracks, nil
}

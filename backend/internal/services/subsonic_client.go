package services

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SubsonicClient handles interaction with any Subsonic-compatible library
// server (Navidrome, Gonic, Airsonic, …). It is the single implementation used
// by the acquisition pipeline for dedup checks, library scans, and agent
// search — previously this logic was duplicated verbatim across GonicClient
// and NavidromeClient.
type SubsonicClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewSubsonicClient creates a new Subsonic-compatible client.
// baseURL is the server root (e.g. http://navidrome:4533); the /rest API
// prefix is appended automatically.
func NewSubsonicClient(baseURL, username, password string, httpClient *http.Client) *SubsonicClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &SubsonicClient{
		baseURL:  fmt.Sprintf("%s/rest", baseURL),
		username: username,
		password: password,
		client:   httpClient,
	}
}

// TriggerScan triggers a full library scan on the server
func (c *SubsonicClient) TriggerScan() (bool, error) {
	var resp struct {
		SubsonicResponse struct {
			Status string `json:"status"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("startScan", nil, &resp)
	if err != nil {
		return false, err
	}

	return resp.SubsonicResponse.Status == "ok", nil
}

// GetScanStatus retrieves the current scan status
func (c *SubsonicClient) GetScanStatus() (map[string]interface{}, error) {
	var resp struct {
		SubsonicResponse struct {
			Status     string `json:"status"`
			ScanStatus struct {
				Scanning bool `json:"scanning"`
				Count    int  `json:"count"`
			} `json:"scanStatus"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("getScanStatus", nil, &resp)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"scanning": resp.SubsonicResponse.ScanStatus.Scanning,
		"count":    resp.SubsonicResponse.ScanStatus.Count,
	}, nil
}

// GetLibraryStats retrieves library statistics from the server
func (c *SubsonicClient) GetLibraryStats() (map[string]int, error) {
	var resp struct {
		SubsonicResponse struct {
			Artists struct {
				Index []struct {
					Artist []struct {
						AlbumCount int `json:"albumCount"`
					} `json:"artist"`
				} `json:"index"`
			} `json:"artists"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("getArtists", nil, &resp)
	if err != nil {
		return nil, err
	}

	artistCount := 0
	albumCount := 0
	for _, idx := range resp.SubsonicResponse.Artists.Index {
		artistCount += len(idx.Artist)
		for _, artist := range idx.Artist {
			albumCount += artist.AlbumCount
		}
	}

	return map[string]int{
		"artist_count": artistCount,
		"album_count":  albumCount,
	}, nil
}

// SubsonicSong represents a track in a Subsonic-compatible server
type SubsonicSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
}

// Search3 searches for tracks, albums or artists
func (c *SubsonicClient) Search3(query string) ([]SubsonicSong, error) {
	params := url.Values{}
	params.Add("query", query)
	params.Add("songCount", "20")

	var resp struct {
		SubsonicResponse struct {
			Status        string `json:"status"`
			SearchResult3 struct {
				Song []SubsonicSong `json:"song"`
			} `json:"searchResult3"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("search3", params, &resp)
	if err != nil {
		return nil, err
	}

	return resp.SubsonicResponse.SearchResult3.Song, nil
}

// GetSong retrieves details for a specific song
func (c *SubsonicClient) GetSong(id string) (*SubsonicSong, error) {
	params := url.Values{}
	params.Add("id", id)

	var resp struct {
		SubsonicResponse struct {
			Status string       `json:"status"`
			Song   SubsonicSong `json:"song"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("getSong", params, &resp)
	if err != nil {
		return nil, err
	}

	if resp.SubsonicResponse.Status != "ok" {
		return nil, fmt.Errorf("subsonic error: %s", resp.SubsonicResponse.Status)
	}

	return &resp.SubsonicResponse.Song, nil
}

func (c *SubsonicClient) doRequest(endpoint string, params url.Values, target interface{}) error {
	if params == nil {
		params = url.Values{}
	}

	// Subsonic token-based auth (keeps password out of URL query params / server logs)
	s, err := salt()
	if err != nil {
		return err
	}
	params.Add("u", c.username)
	params.Add("t", tokenFromPassword(c.password, s))
	params.Add("s", s)
	params.Add("v", "1.16.1")
	params.Add("c", "netrunner")
	params.Add("f", "json")

	fullURL := fmt.Sprintf("%s/%s?%s", c.baseURL, endpoint, params.Encode())

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subsonic api error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// HealthCheck checks if the Subsonic-compatible server is accessible
func (c *SubsonicClient) HealthCheck() bool {
	var resp struct {
		SubsonicResponse struct {
			Status string `json:"status"`
		} `json:"subsonic-response"`
	}

	err := c.doRequest("ping", nil, &resp)
	if err != nil {
		return false
	}

	return resp.SubsonicResponse.Status == "ok"
}

// tokenFromPassword generates a Subsonic token = hex(md5(password + salt)).
func tokenFromPassword(password, s string) string {
	h := md5.Sum([]byte(password + s))
	return hex.EncodeToString(h[:])
}

// salt generates a random hex string for Subsonic token-based auth.
func salt() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("salt generation failed: %w", err)
	}
	return hex.EncodeToString(b), nil
}

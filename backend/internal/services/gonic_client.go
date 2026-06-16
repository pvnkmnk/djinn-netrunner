package services

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// GonicClient handles interaction with the Gonic Subsonic-compatible server
type GonicClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewGonicClient creates a new Gonic client
func NewGonicClient(baseURL, username, password string, httpClient *http.Client) *GonicClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &GonicClient{
		baseURL:  fmt.Sprintf("%s/rest", baseURL),
		username: username,
		password: password,
		client:   httpClient,
	}
}

// TriggerScan triggers a full library scan in Gonic
func (c *GonicClient) TriggerScan() (bool, error) {
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
func (c *GonicClient) GetScanStatus() (map[string]interface{}, error) {
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

// GetLibraryStats retrieves library statistics from Gonic
func (c *GonicClient) GetLibraryStats() (map[string]int, error) {
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

// GonicSong represents a track in Gonic
type GonicSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
}

// Search3 searches for tracks, albums or artists
func (c *GonicClient) Search3(query string) ([]GonicSong, error) {
	params := url.Values{}
	params.Add("query", query)
	params.Add("songCount", "20")

	var resp struct {
		SubsonicResponse struct {
			Status        string `json:"status"`
			SearchResult3 struct {
				Song []GonicSong `json:"song"`
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
func (c *GonicClient) GetSong(id string) (*GonicSong, error) {
	params := url.Values{}
	params.Add("id", id)

	var resp struct {
		SubsonicResponse struct {
			Status string    `json:"status"`
			Song   GonicSong `json:"song"`
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

func (c *GonicClient) doRequest(endpoint string, params url.Values, target interface{}) error {
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gonic api error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *GonicClient) HealthCheck() bool {
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

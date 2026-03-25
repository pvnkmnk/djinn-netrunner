package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// NavidromeClient handles interaction with the Navidrome Subsonic-compatible server
type NavidromeClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewNavidromeClient creates a new Navidrome client
func NewNavidromeClient(baseURL, username, password string) *NavidromeClient {
	return &NavidromeClient{
		baseURL:  fmt.Sprintf("%s/rest", baseURL),
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// TriggerScan triggers a full library scan in Navidrome
func (c *NavidromeClient) TriggerScan() (bool, error) {
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
func (c *NavidromeClient) GetScanStatus() (map[string]interface{}, error) {
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

// GetLibraryStats retrieves library statistics from Navidrome
func (c *NavidromeClient) GetLibraryStats() (map[string]int, error) {
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

// NavidromeSong represents a track in Navidrome
type NavidromeSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
}

// Search3 searches for tracks, albums or artists
func (c *NavidromeClient) Search3(query string) ([]NavidromeSong, error) {
	params := url.Values{}
	params.Add("query", query)
	params.Add("songCount", "20")

	var resp struct {
		SubsonicResponse struct {
			Status        string `json:"status"`
			SearchResult3 struct {
				Song []NavidromeSong `json:"song"`
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
func (c *NavidromeClient) GetSong(id string) (*NavidromeSong, error) {
	params := url.Values{}
	params.Add("id", id)

	var resp struct {
		SubsonicResponse struct {
			Status string        `json:"status"`
			Song   NavidromeSong `json:"song"`
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

func (c *NavidromeClient) doRequest(endpoint string, params url.Values, target interface{}) error {
	if params == nil {
		params = url.Values{}
	}

	params.Add("u", c.username)
	params.Add("p", c.password)
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
		return fmt.Errorf("navidrome api error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// HealthCheck checks if Navidrome is accessible
func (c *NavidromeClient) HealthCheck() bool {
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

// PlexClient handles interaction with Plex Media Server
type PlexClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewPlexClient creates a new Plex client
func NewPlexClient(baseURL, token string) *PlexClient {
	return &PlexClient{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TriggerLibraryRefresh triggers a library refresh in Plex
func (c *PlexClient) TriggerLibraryRefresh(sectionID int) error {
	url := fmt.Sprintf("%s/library/sections/%d/refresh?X-Plex-Token=%s", c.baseURL, sectionID, c.token)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("plex api error: %s", resp.Status)
	}

	return nil
}

// JellyfinClient handles interaction with Jellyfin Media Server
type JellyfinClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewJellyfinClient creates a new Jellyfin client
func NewJellyfinClient(baseURL, apiKey string) *JellyfinClient {
	return &JellyfinClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TriggerLibraryRefresh triggers a library refresh in Jellyfin
func (c *JellyfinClient) TriggerLibraryRefresh() error {
	url := fmt.Sprintf("%s/Library/Refresh", c.baseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	if c.apiKey != "" {
		req.Header.Set("X-Emby-Token", c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("jellyfin api error: %s", resp.Status)
	}

	return nil
}

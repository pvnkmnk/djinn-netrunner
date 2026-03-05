package services

import (
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
func NewGonicClient(baseURL, username, password string) *GonicClient {
	return &GonicClient{
		baseURL:  fmt.Sprintf("%s/rest", baseURL),
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
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

func (c *GonicClient) doRequest(endpoint string, params url.Values, target interface{}) error {
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

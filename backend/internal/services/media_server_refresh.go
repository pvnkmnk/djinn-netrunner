package services

import (
	"fmt"
	"net/http"
	"time"
)

// PlexClient handles interaction with Plex Media Server
type PlexClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewPlexClient creates a new Plex client
func NewPlexClient(baseURL, token string, httpClient *http.Client) *PlexClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &PlexClient{
		baseURL: baseURL,
		token:   token,
		client:  httpClient,
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
func NewJellyfinClient(baseURL, apiKey string, httpClient *http.Client) *JellyfinClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &JellyfinClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  httpClient,
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

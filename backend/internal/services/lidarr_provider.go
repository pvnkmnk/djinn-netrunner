package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// LidarrProvider implements WatchlistProvider for Lidarr sources
type LidarrProvider struct {
	BaseURL string
	APIKey  string
}

// NewLidarrProvider creates a new Lidarr provider
func NewLidarrProvider(baseURL, apiKey string) *LidarrProvider {
	return &LidarrProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

// lidarrAlbum represents an album from Lidarr API
type lidarrAlbum struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	ArtistID    int    `json:"artistId"`
	ReleaseDate string `json:"releaseDate"`
}

// lidarrArtist represents an artist from Lidarr API
type lidarrArtist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// lidarrWantedResponse represents the response from Lidarr wanted/missing endpoint
type lidarrWantedResponse struct {
	Records []lidarrAlbum `json:"records"`
}

// lidarrArtistResponse represents the response from Lidarr artist endpoint
type lidarrArtistResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (p *LidarrProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.SourceType != "lidarr_wanted" {
		return nil, "", fmt.Errorf("unsupported lidarr source type: %s", watchlist.SourceType)
	}

	// Parse base URL
	baseURL, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, "", err
	}

	// Fetch wanted/missing albums
	wantedURL := *baseURL
	wantedURL.Path = "/api/v1/wanted/missing"

	req, err := http.NewRequestWithContext(ctx, "GET", wantedURL.String(), nil)
	if err != nil {
		return nil, "", err
	}

	if p.APIKey != "" {
		req.Header.Set("X-Api-Key", p.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("lidarr api returned status: %d", resp.StatusCode)
	}

	var wantedResp lidarrWantedResponse
	if err := json.NewDecoder(resp.Body).Decode(&wantedResp); err != nil {
		return nil, "", err
	}

	// Fetch artist information for each album
	var allTracks []map[string]string
	artistCache := make(map[int]string)

	for _, album := range wantedResp.Records {
		// Get artist name (with caching)
		artistName, ok := artistCache[album.ArtistID]
		if !ok {
			artistName, err = p.fetchArtistName(ctx, baseURL, album.ArtistID)
			if err != nil {
				artistName = "Unknown Artist"
			}
			artistCache[album.ArtistID] = artistName
		}

		allTracks = append(allTracks, map[string]string{
			"artist": artistName,
			"title":  album.Title,
			"album":  album.Title,
		})
	}

	// Create snapshot ID based on count
	snapshotID := fmt.Sprintf("lidarr:wanted:%d", len(allTracks))
	return allTracks, snapshotID, nil
}

// fetchArtistName fetches artist name from Lidarr API
func (p *LidarrProvider) fetchArtistName(ctx context.Context, baseURL *url.URL, artistID int) (string, error) {
	artistURL := *baseURL
	artistURL.Path = fmt.Sprintf("/api/v1/artist/%d", artistID)

	req, err := http.NewRequestWithContext(ctx, "GET", artistURL.String(), nil)
	if err != nil {
		return "", err
	}

	if p.APIKey != "" {
		req.Header.Set("X-Api-Key", p.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lidarr api returned status: %d", resp.StatusCode)
	}

	var artist lidarrArtistResponse
	if err := json.NewDecoder(resp.Body).Decode(&artist); err != nil {
		return "", err
	}

	return artist.Name, nil
}

func (p *LidarrProvider) ValidateConfig(config string) error {
	// Validate that base URL is provided
	if p.BaseURL == "" {
		return fmt.Errorf("lidarr base URL is required")
	}
	return nil
}

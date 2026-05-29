package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// LastFMProvider implements WatchlistProvider for Last.fm sources
type LastFMProvider struct {
	APIKey  string
	BaseURL string // For testing
}

// NewLastFMProvider creates a new Last.fm provider
func NewLastFMProvider(apiKey string) *LastFMProvider {
	return &LastFMProvider{
		APIKey:  apiKey,
		BaseURL: "http://ws.audioscrobbler.com/2.0/",
	}
}

type lastFMTrack struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	Image []struct {
		Text string `json:"#text"`
		Size string `json:"size"`
	} `json:"image"`
}

type lastFMLovedResponse struct {
	LovedTracks struct {
		Track []lastFMTrack `json:"track"`
		Attr  struct {
			Total string `json:"total"`
		} `json:"@attr"`
	} `json:"lovedtracks"`
}

type lastFMTopResponse struct {
	TopTracks struct {
		Track []lastFMTrack `json:"track"`
		Attr  struct {
			Total string `json:"total"`
		} `json:"@attr"`
	} `json:"toptracks"`
}

func (p *LastFMProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	method := ""
	if watchlist.SourceType == "lastfm_loved" {
		method = "user.getlovedtracks"
	} else if watchlist.SourceType == "lastfm_top" {
		method = "user.gettoptracks"
	} else {
		return nil, "", fmt.Errorf("unsupported lastfm source type: %s", watchlist.SourceType)
	}

	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, "", err
	}

	q := u.Query()
	q.Set("method", method)
	q.Set("user", watchlist.SourceURI)
	q.Set("api_key", p.APIKey)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("last.fm api returned status: %d", resp.StatusCode)
	}

	var allTracks []map[string]string
	var snapshotID string

	if watchlist.SourceType == "lastfm_loved" {
		var data lastFMLovedResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, "", err
		}
		for _, t := range data.LovedTracks.Track {
			allTracks = append(allTracks, p.mapTrack(t))
		}
		snapshotID = fmt.Sprintf("loved:%s", data.LovedTracks.Attr.Total)
	} else {
		var data lastFMTopResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, "", err
		}
		for _, t := range data.TopTracks.Track {
			allTracks = append(allTracks, p.mapTrack(t))
		}
		snapshotID = fmt.Sprintf("top:%s", data.TopTracks.Attr.Total)
	}

	return allTracks, snapshotID, nil
}

func (p *LastFMProvider) mapTrack(t lastFMTrack) map[string]string {
	coverURL := ""
	for _, img := range t.Image {
		if img.Size == "extralarge" || img.Size == "large" {
			coverURL = img.Text
		}
	}
	return map[string]string{
		"artist":        t.Artist.Name,
		"title":         t.Name,
		"album":         t.Album.Name,
		"cover_art_url": coverURL,
	}
}

func (p *LastFMProvider) ValidateConfig(config string) error {
	// API key is required globally, but we might want per-watchlist config too
	return nil
}

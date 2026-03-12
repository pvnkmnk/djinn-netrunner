package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// ListenBrainzProvider implements WatchlistProvider for ListenBrainz sources
type ListenBrainzProvider struct {
	Token   string
	BaseURL string // For testing
}

// NewListenBrainzProvider creates a new ListenBrainz provider
func NewListenBrainzProvider(token string) *ListenBrainzProvider {
	return &ListenBrainzProvider{
		Token:   token,
		BaseURL: "https://api.listenbrainz.org/1/",
	}
}

type lbTrackMetadata struct {
	ArtistName     string `json:"artist_name"`
	TrackName      string `json:"track_name"`
	ReleaseName    string `json:"release_name"`
	AdditionalInfo struct {
		RecordingMSID string `json:"recording_msid"`
	} `json:"additional_info"`
}

type lbListen struct {
	TrackMetadata lbTrackMetadata `json:"track_metadata"`
	ListenedAt    int64           `json:"listened_at"`
}

type lbListensResponse struct {
	Payload struct {
		Listens []lbListen `json:"listens"`
		Count   int        `json:"count"`
		UserID  string     `json:"user_id"`
	} `json:"payload"`
}

func (p *ListenBrainzProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.SourceType != "listenbrainz_listens" {
		return nil, "", fmt.Errorf("unsupported listenbrainz source type: %s", watchlist.SourceType)
	}

	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, "", err
	}
	
	// Ensure trailing slash for joining
	if u.Path != "" && u.Path[len(u.Path)-1] != '/' {
		u.Path += "/"
	}
	u.Path += "user/" + watchlist.SourceURI + "/listens"

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, "", err
	}

	if p.Token != "" {
		req.Header.Set("Authorization", "Token "+p.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("listenbrainz api returned status: %d", resp.StatusCode)
	}

	var data lbListensResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "", err
	}

	var allTracks []map[string]string
	var maxTimestamp int64

	for _, l := range data.Payload.Listens {
		allTracks = append(allTracks, map[string]string{
			"artist": l.TrackMetadata.ArtistName,
			"title":  l.TrackMetadata.TrackName,
			"album":  l.TrackMetadata.ReleaseName,
		})
		if l.ListenedAt > maxTimestamp {
			maxTimestamp = l.ListenedAt
		}
	}

	snapshotID := fmt.Sprintf("listens:%d", maxTimestamp)
	return allTracks, snapshotID, nil
}

func (p *ListenBrainzProvider) ValidateConfig(config string) error {
	return nil
}

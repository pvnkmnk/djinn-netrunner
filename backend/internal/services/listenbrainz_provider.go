package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// ListenBrainzProvider implements WatchlistProvider for ListenBrainz sources
type ListenBrainzProvider struct {
	Token      string
	BaseURL    string // For testing
	httpClient *http.Client
}

// NewListenBrainzProvider creates a new ListenBrainz provider
func NewListenBrainzProvider(token string, httpClient *http.Client) *ListenBrainzProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ListenBrainzProvider{
		Token:      token,
		BaseURL:    "https://api.listenbrainz.org/1/",
		httpClient: httpClient,
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

	const (
		perPage  = 100
		maxPages = 10
	)

	baseURL, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, "", err
	}
	if baseURL.Path != "" && baseURL.Path[len(baseURL.Path)-1] != '/' {
		baseURL.Path += "/"
	}
	baseURL.Path += "user/" + watchlist.SourceURI + "/listens"
	endpoint := baseURL.String()

	var allTracks []map[string]string
	var maxTimestamp int64
	var maxTS int64

	for page := 0; page < maxPages; page++ {
		reqURL, err := url.Parse(endpoint)
		if err != nil {
			return nil, "", err
		}

		q := reqURL.Query()
		q.Set("count", strconv.Itoa(perPage))
		if maxTS > 0 {
			q.Set("max_ts", strconv.FormatInt(maxTS, 10))
		}
		reqURL.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, "GET", reqURL.String(), nil)
		if err != nil {
			return nil, "", err
		}

		if p.Token != "" {
			req.Header.Set("Authorization", "Token "+p.Token)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, "", err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, "", fmt.Errorf("listenbrainz api returned status: %d", resp.StatusCode)
		}

		var data lbListensResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			return nil, "", err
		}
		resp.Body.Close()

		if len(data.Payload.Listens) == 0 {
			break
		}

		var minTS int64
		for _, l := range data.Payload.Listens {
			allTracks = append(allTracks, map[string]string{
				"artist": l.TrackMetadata.ArtistName,
				"title":  l.TrackMetadata.TrackName,
				"album":  l.TrackMetadata.ReleaseName,
			})
			if l.ListenedAt > maxTimestamp {
				maxTimestamp = l.ListenedAt
			}
			if minTS == 0 || l.ListenedAt < minTS {
				minTS = l.ListenedAt
			}
		}

		if len(data.Payload.Listens) < perPage {
			break
		}

		maxTS = minTS
	}

	snapshotID := fmt.Sprintf("listens:%d", maxTimestamp)
	return allTracks, snapshotID, nil
}

func (p *ListenBrainzProvider) ValidateConfig(config string) error {
	if config == "" {
		return fmt.Errorf("listenbrainz username must not be empty")
	}
	return nil
}

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

// LastFMProvider implements WatchlistProvider for Last.fm sources
type LastFMProvider struct {
	APIKey     string
	BaseURL    string // For testing
	httpClient *http.Client
}

// NewLastFMProvider creates a new Last.fm provider
func NewLastFMProvider(apiKey string, httpClient *http.Client) *LastFMProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &LastFMProvider{
		APIKey:     apiKey,
		BaseURL:    "http://ws.audioscrobbler.com/2.0/",
		httpClient: httpClient,
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

	const perPage = 200
	var allTracks []map[string]string
	var totalStr string

	for page := 1; ; page++ {
		u, err := url.Parse(p.BaseURL)
		if err != nil {
			return nil, "", err
		}

		q := u.Query()
		q.Set("method", method)
		q.Set("user", watchlist.SourceURI)
		q.Set("api_key", p.APIKey)
		q.Set("format", "json")
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", strconv.Itoa(perPage))
		if watchlist.SourceType == "lastfm_top" {
			q.Set("period", "overall")
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, "", err
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, "", err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, "", fmt.Errorf("last.fm api returned status: %d", resp.StatusCode)
		}

		var pageTracks []lastFMTrack
		var total int

		if watchlist.SourceType == "lastfm_loved" {
			var data lastFMLovedResponse
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				resp.Body.Close()
				return nil, "", err
			}
			pageTracks = data.LovedTracks.Track
			totalStr = data.LovedTracks.Attr.Total
			total, err = strconv.Atoi(totalStr)
			if err != nil {
				resp.Body.Close()
				return nil, "", fmt.Errorf("invalid total in lastfm response: %q", totalStr)
			}
		} else {
			var data lastFMTopResponse
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				resp.Body.Close()
				return nil, "", err
			}
			pageTracks = data.TopTracks.Track
			totalStr = data.TopTracks.Attr.Total
			total, err = strconv.Atoi(totalStr)
			if err != nil {
				resp.Body.Close()
				return nil, "", fmt.Errorf("invalid total in lastfm response: %q", totalStr)
			}
		}
		resp.Body.Close()

		for _, t := range pageTracks {
			allTracks = append(allTracks, p.mapTrack(t))
		}

		if len(allTracks) >= total || len(pageTracks) < perPage {
			break
		}
	}

	var snapshotID string
	if watchlist.SourceType == "lastfm_loved" {
		snapshotID = fmt.Sprintf("loved:%s", totalStr)
	} else {
		snapshotID = fmt.Sprintf("top:%s", totalStr)
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
	if config == "" {
		return fmt.Errorf("lastfm username must not be empty")
	}
	return nil
}

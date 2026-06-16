package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	var method string
	switch watchlist.SourceType {
	case "lastfm_loved":
		method = "user.getlovedtracks"
	case "lastfm_top":
		method = "user.gettoptracks"
	default:
		return nil, "", fmt.Errorf("unsupported lastfm source type: %s", watchlist.SourceType)
	}

	const perPage = 200
	var allTracks []map[string]string
	var allRawTracks []lastFMTrack

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
			return nil, "", classifyNetworkError(err, "last.fm")
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, "", classifyHTTPStatus(resp.StatusCode, "last.fm")
		}

		var pageTracks []lastFMTrack
		var total int

		if watchlist.SourceType == "lastfm_loved" {
			var data lastFMLovedResponse
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				_ = resp.Body.Close()
				return nil, "", err
			}
			pageTracks = data.LovedTracks.Track
			total, err = strconv.Atoi(data.LovedTracks.Attr.Total)
			if err != nil {
				_ = resp.Body.Close()
				return nil, "", fmt.Errorf("invalid total in lastfm response: %q", data.LovedTracks.Attr.Total)
			}
		} else {
			var data lastFMTopResponse
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				_ = resp.Body.Close()
				return nil, "", err
			}
			pageTracks = data.TopTracks.Track
			total, err = strconv.Atoi(data.TopTracks.Attr.Total)
			if err != nil {
				_ = resp.Body.Close()
				return nil, "", fmt.Errorf("invalid total in lastfm response: %q", data.TopTracks.Attr.Total)
			}
		}
		_ = resp.Body.Close()

		for _, t := range pageTracks {
			allTracks = append(allTracks, p.mapTrack(t))
		}
		allRawTracks = append(allRawTracks, pageTracks...)

		if len(allTracks) >= total || len(pageTracks) < perPage {
			break
		}
	}

	var snapshotID string
	if watchlist.SourceType == "lastfm_loved" {
		snapshotID = fmt.Sprintf("loved:%s", hashTrackList(allRawTracks))
	} else {
		snapshotID = fmt.Sprintf("top:%s", hashTrackList(allRawTracks))
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

// hashTrackList computes a deterministic hash of a Last.fm track list.
func hashTrackList(tracks []lastFMTrack) string {
	h := sha256.New()
	for _, t := range tracks {
		h.Write([]byte(t.Artist.Name))
		h.Write([]byte(t.Name))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (p *LastFMProvider) ValidateConfig(config string) error {
	if config == "" {
		return fmt.Errorf("lastfm username must not be empty")
	}
	return nil
}

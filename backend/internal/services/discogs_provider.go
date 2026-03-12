package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// DiscogsProvider implements WatchlistProvider for Discogs sources
type DiscogsProvider struct {
	Token   string
	BaseURL string // For testing
}

// NewDiscogsProvider creates a new Discogs provider
func NewDiscogsProvider(token string) *DiscogsProvider {
	return &DiscogsProvider{
		Token:   token,
		BaseURL: "https://api.discogs.com/",
	}
}

type discogsWant struct {
	BasicInformation struct {
		Title   string `json:"title"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		CoverImage string `json:"cover_image"`
	} `json:"basic_information"`
	DateAdded string `json:"date_added"`
}

type discogsWantlistResponse struct {
	Pagination struct {
		Items int `json:"items"`
	} `json:"pagination"`
	Wants []discogsWant `json:"wants"`
}

func (p *DiscogsProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.SourceType != "discogs_wantlist" {
		return nil, "", fmt.Errorf("unsupported discogs source type: %s", watchlist.SourceType)
	}

	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, "", err
	}
	
	u.Path = fmt.Sprintf("/users/%s/wants", watchlist.SourceURI)

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, "", err
	}

	if p.Token != "" {
		req.Header.Set("Authorization", "Discogs token="+p.Token)
	}
	req.Header.Set("User-Agent", "NetRunner/1.0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("discogs api returned status: %d", resp.StatusCode)
	}

	var data discogsWantlistResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "", err
	}

	var allTracks []map[string]string
	var lastAdded string

	for _, w := range data.Wants {
		artistName := ""
		if len(w.BasicInformation.Artists) > 0 {
			artistName = w.BasicInformation.Artists[0].Name
		}

		allTracks = append(allTracks, map[string]string{
			"artist":        artistName,
			"title":         w.BasicInformation.Title,
			"cover_art_url": w.BasicInformation.CoverImage,
		})

		if w.DateAdded > lastAdded {
			lastAdded = w.DateAdded
		}
	}

	snapshotID := fmt.Sprintf("wantlist:%s", lastAdded)
	return allTracks, snapshotID, nil
}

func (p *DiscogsProvider) ValidateConfig(config string) error {
	return nil
}

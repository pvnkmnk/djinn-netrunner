package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmcdole/gofeed"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// RSSProvider implements WatchlistProvider for RSS/Atom feeds
type RSSProvider struct {
	httpClient *http.Client
}

// NewRSSProvider creates a new RSS provider with the given HTTP client.
func NewRSSProvider(httpClient *http.Client) *RSSProvider {
	return &RSSProvider{httpClient: httpClient}
}

func (p *RSSProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	fp := gofeed.NewParser()
	if p.httpClient != nil {
		fp.Client = p.httpClient
	}
	feed, err := fp.ParseURLWithContext(watchlist.SourceURI, ctx)
	if err != nil {
		return nil, "", classifyNetworkError(err, "rss")
	}

	var allTracks []map[string]string
	var lastUpdate string

	for _, item := range feed.Items {
		// Try to parse "Artist - Title" from item title
		artist := ""
		title := item.Title
		
		parts := strings.SplitN(item.Title, " - ", 2)
		if len(parts) == 2 {
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		} else {
			// Fallback: use feed/channel title as artist (for Bandcamp-style feeds)
			artist = strings.TrimSpace(feed.Title)
		}

		coverURL := ""
		// Try to find image in extensions (e.g., media:content)
		if media, ok := item.Extensions["media"]; ok {
			if content, ok := media["content"]; ok && len(content) > 0 {
				coverURL = content[0].Attrs["url"]
			}
		}
		
		// Fallback to item.Image or feed.Image
		if coverURL == "" && item.Image != nil {
			coverURL = item.Image.URL
		}

		allTracks = append(allTracks, map[string]string{
			"artist":        artist,
			"title":         title,
			"cover_art_url": coverURL,
			"source_link":   item.Link,
		})

		// Use the most recent pub date as snapshot ID
		if item.Published != "" && (lastUpdate == "" || item.Published > lastUpdate) {
			lastUpdate = item.Published
		}
	}

	if lastUpdate == "" {
		lastUpdate = feed.Updated
	}
	if lastUpdate == "" {
		lastUpdate = fmt.Sprintf("count:%d", len(allTracks))
	}

	return allTracks, lastUpdate, nil
}

func (p *RSSProvider) ValidateConfig(config string) error {
	return nil
}

package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/zmb3/spotify/v2"
)

// SpotifyProvider implements WatchlistProvider for Spotify sources
type SpotifyProvider struct {
	auth *api.SpotifyAuthHandler
}

// NewSpotifyProvider creates a new Spotify provider
func NewSpotifyProvider(auth *api.SpotifyAuthHandler) *SpotifyProvider {
	return &SpotifyProvider{
		auth: auth,
	}
}

// FetchTracks retrieves tracks from a Spotify source
func (p *SpotifyProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.OwnerUserID == nil {
		return nil, "", errors.New("watchlist has no owner user")
	}

	client, err := p.auth.GetClient(ctx, *watchlist.OwnerUserID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get spotify client: %w", err)
	}

	var allTracks []map[string]string
	var snapshotID string

	if watchlist.SourceType == "spotify_liked" {
		// Fetch Liked Songs (Saved Tracks)
		limit := 50
		offset := 0
		for {
			page, err := client.CurrentUsersTracks(ctx, spotify.Limit(limit), spotify.Offset(offset))
			if err != nil {
				return nil, "", err
			}

			for _, item := range page.Tracks {
				artistName := ""
				if len(item.Artists) > 0 {
					artistName = item.Artists[0].Name
				}
				coverURL := ""
				if len(item.Album.Images) > 0 {
					coverURL = item.Album.Images[0].URL
				}
				allTracks = append(allTracks, map[string]string{
					"id":            string(item.ID),
					"artist":        artistName,
					"title":         item.Name,
					"album":         item.Album.Name,
					"cover_art_url": coverURL,
				})
			}

			offset += len(page.Tracks)
			if offset >= int(page.Total) {
				break
			}
		}
		snapshotID = fmt.Sprintf("liked:%d", len(allTracks))

	} else if watchlist.SourceType == "spotify_playlist" {
		// Fetch Playlist tracks
		playlistID := p.ExtractPlaylistID(watchlist.SourceURI)
		id := spotify.ID(playlistID)

		// Get playlist metadata for snapshot ID
		playlist, err := client.GetPlaylist(ctx, id, spotify.Fields("snapshot_id"))
		if err != nil {
			return nil, "", err
		}
		snapshotID = playlist.SnapshotID

		limit := 100
		offset := 0
		for {
			page, err := client.GetPlaylistItems(ctx, id, spotify.Limit(limit), spotify.Offset(offset))
			if err != nil {
				return nil, "", err
			}

			for _, item := range page.Items {
				if item.Track.Track == nil {
					continue
				}
				t := item.Track.Track
				artistName := ""
				if len(t.Artists) > 0 {
					artistName = t.Artists[0].Name
				}
				coverURL := ""
				if len(t.Album.Images) > 0 {
					coverURL = t.Album.Images[0].URL
				}
				allTracks = append(allTracks, map[string]string{
					"id":            string(t.ID),
					"artist":        artistName,
					"title":         t.Name,
					"album":         t.Album.Name,
					"cover_art_url": coverURL,
				})
			}

			offset += len(page.Items)
			if offset >= int(page.Total) {
				break
			}
		}
	} else {
		return nil, "", fmt.Errorf("unsupported spotify source type: %s", watchlist.SourceType)
	}

	return allTracks, snapshotID, nil
}

// ValidateConfig checks if the config is valid (not much to check for Spotify yet)
func (p *SpotifyProvider) ValidateConfig(config string) error {
	return nil
}

// ExtractPlaylistID extracts the ID from a Spotify URI or URL
func (p *SpotifyProvider) ExtractPlaylistID(uri string) string {
	if strings.HasPrefix(uri, "spotify:playlist:") {
		return strings.TrimPrefix(uri, "spotify:playlist:")
	}
	if strings.Contains(uri, "open.spotify.com/playlist/") {
		parts := strings.Split(uri, "/playlist/")
		if len(parts) > 1 {
			id := strings.Split(parts[1], "?")[0]
			return strings.Split(id, "#")[0]
		}
	}
	return uri
}

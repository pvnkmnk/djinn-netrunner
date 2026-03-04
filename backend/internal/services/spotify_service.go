package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

type SpotifyService struct {
	cfg    *config.Config
	client *spotify.Client
}

func NewSpotifyService(cfg *config.Config) *SpotifyService {
	if cfg.SpotifyClientID == "" || cfg.SpotifyClientSecret == "" {
		return &SpotifyService{cfg: cfg}
	}

	ctx := context.Background()
	authCfg := &clientcredentials.Config{
		ClientID:     cfg.SpotifyClientID,
		ClientSecret: cfg.SpotifyClientSecret,
		TokenURL:     spotifyauth.TokenURL,
	}
	
	httpClient := authCfg.Client(ctx)
	client := spotify.New(httpClient)

	return &SpotifyService{
		cfg:    cfg,
		client: client,
	}
}

func (s *SpotifyService) GetPlaylistTracks(ctx context.Context, playlistID string) ([]map[string]string, error) {
	if s.client == nil {
		return nil, fmt.Errorf("spotify client not initialized")
	}

	id := spotify.ID(playlistID)
	items, err := s.client.GetPlaylistItems(ctx, id)
	if err != nil {
		return nil, err
	}

	var tracks []map[string]string
	for _, item := range items.Items {
		if item.Track.Track == nil {
			continue
		}
		t := item.Track.Track
		artistName := ""
		if len(t.Artists) > 0 {
			artistName = t.Artists[0].Name
		}

		tracks = append(tracks, map[string]string{
			"artist": artistName,
			"title":  t.Name,
			"album":  t.Album.Name,
		})
	}

	return tracks, nil
}

func (s *SpotifyService) ExtractPlaylistID(uri string) string {
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

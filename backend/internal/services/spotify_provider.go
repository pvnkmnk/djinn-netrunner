package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/interfaces"
	"github.com/zmb3/spotify/v2"
)

// SpotifyProvider implements WatchlistProvider for Spotify sources using a two-pronged strategy:
//
// Prong 1 — Client Credentials (public data): Uses well-known client credentials to
// access api.spotify.com/v1 for public playlists. No user login required.
//
// Prong 2 — sp_dc cookie (user-specific data): Uses the user's sp_dc browser cookie
// to access the GraphQL Partner API for private playlists, Liked Songs, and
// algorithmic playlists (Discover Weekly, Daily Mixes, etc.).
//
// Legacy — OAuth (backward compat): Falls back to the existing zmb3/spotify OAuth
// client for users who have already linked via the Developer App flow.
type SpotifyProvider struct {
	auth  interfaces.SpotifyClientProvider // Legacy OAuth client (may be nil)
	spdc  *SpDcAuth                        // sp_dc cookie auth (may be nil)
}

// NewSpotifyProvider creates a new Spotify provider with legacy OAuth support.
func NewSpotifyProvider(auth interfaces.SpotifyClientProvider) *SpotifyProvider {
	return &SpotifyProvider{
		auth: auth,
	}
}

// NewSpotifyProviderWithSpDc creates a new Spotify provider with sp_dc support.
func NewSpotifyProviderWithSpDc(auth interfaces.SpotifyClientProvider, spdc *SpDcAuth) *SpotifyProvider {
	return &SpotifyProvider{
		auth: auth,
		spdc: spdc,
	}
}

// FetchTracks retrieves tracks from a Spotify source.
//
// For spotify_playlist: tries Prong 1 (client credentials / public Web API) first,
// falls back to Prong 2 (sp_dc GraphQL), then Legacy (OAuth).
//
// For spotify_liked: tries Prong 2 (sp_dc GraphQL) first, then Legacy (OAuth).
// Client credentials cannot access user-specific data.
//
// For spotify_discover: uses Prong 2 only (sp_dc GraphQL home feed).
func (p *SpotifyProvider) FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	switch watchlist.SourceType {
	case "spotify_playlist":
		return p.fetchPlaylistTracks(ctx, watchlist)
	case "spotify_liked":
		return p.fetchLikedSongs(ctx, watchlist)
	case "spotify_discover":
		return p.fetchDiscoverTracks(ctx, watchlist)
	default:
		return nil, "", fmt.Errorf("unsupported spotify source type: %s", watchlist.SourceType)
	}
}

func (p *SpotifyProvider) fetchPlaylistTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	playlistID := ExtractSpotifyPlaylistID(watchlist.SourceURI)

	// Prong 1: Try client credentials + public Web API (no user auth needed)
	if p.spdc != nil {
		tracks, err := p.spdc.FetchPlaylistTracksViaWebAPI(playlistID)
		if err == nil && len(tracks) > 0 {
			slog.Debug("spotify playlist fetched via client credentials (Prong 1)",
				"playlistID", playlistID, "tracks", len(tracks))
			snapshotID := fmt.Sprintf("cc:%d", len(tracks))
			return tracks, snapshotID, nil
		}
		if err != nil {
			slog.Debug("client credentials fetch failed, trying sp_dc fallback",
				"playlistID", playlistID, "error", err)
		}

		// Prong 2: Try sp_dc GraphQL
		if p.spdc.HasSpDcCookie() {
			gqlTracks, err := p.spdc.FetchPlaylistTracksViaGraphQL(playlistID)
			if err == nil && len(gqlTracks) > 0 {
				slog.Debug("spotify playlist fetched via sp_dc GraphQL (Prong 2)",
					"playlistID", playlistID, "tracks", len(gqlTracks))
				mapped := graphqlTracksToMaps(gqlTracks)
				snapshotID := fmt.Sprintf("gql:%d", len(mapped))
				return mapped, snapshotID, nil
			}
			if err != nil {
				slog.Debug("sp_dc GraphQL fetch failed, trying OAuth fallback",
					"playlistID", playlistID, "error", err)
			}
		}
	}

	// Legacy: OAuth via zmb3/spotify
	return p.fetchPlaylistTracksOAuth(ctx, watchlist)
}

func (p *SpotifyProvider) fetchLikedSongs(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	// Prong 2: sp_dc GraphQL (preferred for liked songs)
	if p.spdc != nil && p.spdc.HasSpDcCookie() {
		var allTracks []SpotifyGraphQLTrack
		limit := 50
		offset := 0
		for {
			tracks, err := p.spdc.FetchLikedSongs(limit, offset)
			if err != nil {
				slog.Debug("sp_dc liked songs fetch failed", "offset", offset, "error", err)
				break
			}
			allTracks = append(allTracks, tracks...)
			if len(tracks) < limit {
				break
			}
			offset += limit
		}
		if len(allTracks) > 0 {
			slog.Debug("spotify liked songs fetched via sp_dc GraphQL",
				"tracks", len(allTracks))
			mapped := graphqlTracksToMaps(allTracks)
			snapshotID := fmt.Sprintf("liked:%d", len(mapped))
			return mapped, snapshotID, nil
		}
	}

	// Legacy: OAuth
	return p.fetchLikedSongsOAuth(ctx, watchlist)
}

func (p *SpotifyProvider) fetchDiscoverTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if p.spdc == nil || !p.spdc.HasSpDcCookie() {
		return nil, "", errors.New("spotify_discover requires sp_dc cookie authentication")
	}

	// Get the target playlist name from SourceURI (e.g., "discover weekly", "daily mix 1")
	targetName := strings.ToLower(strings.TrimSpace(watchlist.SourceURI))

	mixes, err := p.spdc.FetchDailyMixes()
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch home feed: %w", err)
	}

	// Find the matching playlist
	var targetPlaylistID string
	for _, mix := range mixes {
		if strings.ToLower(mix.Name) == targetName {
			targetPlaylistID = mix.ID
			break
		}
	}

	if targetPlaylistID == "" {
		// If no exact match, try prefix match
		for _, mix := range mixes {
			if strings.HasPrefix(strings.ToLower(mix.Name), targetName) {
				targetPlaylistID = mix.ID
				break
			}
		}
	}

	if targetPlaylistID == "" {
		availableNames := make([]string, 0, len(mixes))
		for _, m := range mixes {
			availableNames = append(availableNames, m.Name)
		}
		return nil, "", fmt.Errorf("playlist %q not found in home feed; available: %v", watchlist.SourceURI, availableNames)
	}

	// Fetch tracks from the discovered playlist
	gqlTracks, err := p.spdc.FetchPlaylistTracksViaGraphQL(targetPlaylistID)
	if err != nil {
		// Fall back to Web API for public Spotify playlists
		webTracks, webErr := p.spdc.FetchPlaylistTracksViaWebAPI(targetPlaylistID)
		if webErr != nil {
			return nil, "", fmt.Errorf("failed to fetch discover playlist tracks: %w (web api: %v)", err, webErr)
		}
		return webTracks, fmt.Sprintf("discover:%d", len(webTracks)), nil
	}

	mapped := graphqlTracksToMaps(gqlTracks)
	return mapped, fmt.Sprintf("discover:%d", len(mapped)), nil
}

// fetchPlaylistTracksOAuth uses the legacy OAuth client to fetch playlist tracks.
func (p *SpotifyProvider) fetchPlaylistTracksOAuth(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.OwnerUserID == nil {
		return nil, "", errors.New("watchlist has no owner user (required for OAuth)")
	}

	if p.auth == nil {
		return nil, "", errors.New("no authentication method available for spotify_playlist")
	}

	client, err := p.auth.GetClient(ctx, *watchlist.OwnerUserID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get spotify OAuth client: %w", err)
	}

	playlistID := ExtractSpotifyPlaylistID(watchlist.SourceURI)
	id := spotify.ID(playlistID)

	playlist, err := client.GetPlaylist(ctx, id, spotify.Fields("snapshot_id"))
	if err != nil {
		return nil, "", err
	}
	snapshotID := playlist.SnapshotID

	var allTracks []map[string]string
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

	return allTracks, snapshotID, nil
}

// fetchLikedSongsOAuth uses the legacy OAuth client to fetch liked songs.
func (p *SpotifyProvider) fetchLikedSongsOAuth(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error) {
	if watchlist.OwnerUserID == nil {
		return nil, "", errors.New("watchlist has no owner user (required for OAuth)")
	}

	if p.auth == nil {
		return nil, "", errors.New("no authentication method available for spotify_liked")
	}

	client, err := p.auth.GetClient(ctx, *watchlist.OwnerUserID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get spotify OAuth client: %w", err)
	}

	var allTracks []map[string]string
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

	snapshotID := fmt.Sprintf("liked:%d", len(allTracks))
	return allTracks, snapshotID, nil
}

// ValidateConfig checks if the Spotify source URI is valid for a given source type.
func (p *SpotifyProvider) ValidateConfig(config string) error {
	if config == "" {
		return errors.New("spotify source URI is required")
	}

	// Playlist URIs must contain an extractable ID
	if strings.HasPrefix(config, "spotify:playlist:") || strings.Contains(config, "open.spotify.com/playlist/") {
		id := ExtractSpotifyPlaylistID(config)
		if id == "" {
			return errors.New("could not extract playlist ID from URI")
		}
		return nil
	}

	// Reject URLs that look like Spotify but aren't playlist links
	if strings.Contains(config, "open.spotify.com/") {
		return fmt.Errorf("unsupported Spotify URL format; expected a playlist URL (open.spotify.com/playlist/...)")
	}

	// Allow freeform text for discover/liked types (e.g. "Discover Weekly", "liked")
	return nil
}

// ExtractSpotifyPlaylistID extracts the ID from a Spotify URI or URL.
func ExtractSpotifyPlaylistID(uri string) string {
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

// graphqlTracksToMaps converts GraphQL track structs to the map format expected by the pipeline.
func graphqlTracksToMaps(tracks []SpotifyGraphQLTrack) []map[string]string {
	result := make([]map[string]string, 0, len(tracks))
	for _, t := range tracks {
		result = append(result, map[string]string{
			"id":            t.ID,
			"artist":        t.Artist,
			"title":         t.Name,
			"album":         t.Album,
			"cover_art_url": t.CoverURL,
		})
	}
	return result
}

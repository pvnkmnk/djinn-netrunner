package services

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// SpotifyGraphQLTrack represents a track parsed from GraphQL responses.
type SpotifyGraphQLTrack struct {
	ID       string
	Name     string
	Artist   string
	Album    string
	CoverURL string
	URI      string
}

// SpotifyGraphQLPlaylist represents a playlist from the libraryV3 response.
type SpotifyGraphQLPlaylist struct {
	ID         string
	Name       string
	OwnerID    string
	ImageURL   string
	TrackCount int
}

// FetchUserPlaylists retrieves the user's playlists via GraphQL libraryV3.
func (a *SpDcAuth) FetchUserPlaylists(limit, offset int) ([]SpotifyGraphQLPlaylist, error) {
	variables := fmt.Sprintf(
		`{"filters":["Playlists"],"order":null,"textFilter":"","features":["LIKED_SONGS","YOUR_EPISODES"],"limit":%d,"offset":%d}`,
		limit, offset)

	raw, err := a.ExecuteGraphQL("libraryV3", variables, hashLibraryV3)
	if err != nil {
		return nil, fmt.Errorf("libraryV3 query failed: %w", err)
	}

	return parseLibraryV3Response(raw)
}

// FetchPlaylistTracksViaGraphQL retrieves playlist tracks via the GraphQL fetchPlaylist query.
func (a *SpDcAuth) FetchPlaylistTracksViaGraphQL(playlistID string) ([]SpotifyGraphQLTrack, error) {
	uri := "spotify:playlist:" + playlistID
	var allTracks []SpotifyGraphQLTrack
	offset := 0
	pageSize := 100

	for {
		variables := fmt.Sprintf(
			`{"uri":"%s","offset":%d,"limit":%d,"enableWatchFeedEntrypoint":false}`,
			uri, offset, pageSize)

		raw, err := a.ExecuteGraphQL("fetchPlaylist", variables, hashFetchPlaylist)
		if err != nil {
			if len(allTracks) > 0 {
				slog.Warn("GraphQL fetchPlaylist failed mid-pagination, returning partial results",
					"offset", offset, "tracksCollected", len(allTracks), "error", err)
				return allTracks, nil
			}
			return nil, fmt.Errorf("fetchPlaylist query failed: %w", err)
		}

		tracks, rawCount, err := parsePlaylistTracksResponse(raw)
		if err != nil {
			return nil, err
		}

		allTracks = append(allTracks, tracks...)

		// Use rawCount (not len(tracks)) for page-end detection.
		// Filtered items (local tracks, podcasts) reduce len(tracks) but
		// rawCount reflects the true page size from Spotify.
		if rawCount < pageSize {
			break
		}
		offset += pageSize
	}

	return allTracks, nil
}

// FetchLikedSongs retrieves the user's Liked Songs via GraphQL.
func (a *SpDcAuth) FetchLikedSongs(limit, offset int) ([]SpotifyGraphQLTrack, error) {
	variables := fmt.Sprintf(`{"offset":%d,"limit":%d}`, offset, limit)

	raw, err := a.ExecuteGraphQL("fetchLibraryTracks", variables, hashFetchLibraryTracks)
	if err != nil {
		return nil, fmt.Errorf("fetchLibraryTracks query failed: %w", err)
	}

	return parseLikedSongsResponse(raw)
}

// FetchDailyMixes retrieves Spotify-generated playlists from the home feed.
func (a *SpDcAuth) FetchDailyMixes() ([]SpotifyGraphQLPlaylist, error) {
	spT := a.spTDeviceID
	variables := fmt.Sprintf(
		`{"homeEndUserIntegration":"INTEGRATION_WEB_PLAYER","timeZone":"UTC","sp_t":"%s","facet":null,"sectionItemsLimit":20}`,
		spT)

	raw, err := a.ExecuteGraphQL("home", variables, hashHome)
	if err != nil {
		return nil, fmt.Errorf("home query failed: %w", err)
	}

	return parseHomeFeedResponse(raw)
}

// parseLibraryV3Response parses the libraryV3 GraphQL response.
func parseLibraryV3Response(raw json.RawMessage) ([]SpotifyGraphQLPlaylist, error) {
	var resp struct {
		Data struct {
			Me struct {
				LibraryV3 struct {
					Items []json.RawMessage `json:"items"`
				} `json:"libraryV3"`
			} `json:"me"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse libraryV3 response: %w", err)
	}

	var playlists []SpotifyGraphQLPlaylist
	for _, itemRaw := range resp.Data.Me.LibraryV3.Items {
		var wrapper struct {
			Item struct {
				Data struct {
					Typename string `json:"__typename"`
					URI      string `json:"uri"`
					Name     string `json:"name"`
					OwnerV2  struct {
						Data struct {
							Username string `json:"username"`
						} `json:"data"`
					} `json:"ownerV2"`
					Images struct {
						Items []struct {
							Sources []struct {
								URL string `json:"url"`
							} `json:"sources"`
						} `json:"items"`
					} `json:"images"`
					Content struct {
						TotalCount int `json:"totalCount"`
					} `json:"content"`
				} `json:"data"`
			} `json:"item"`
		}

		if err := json.Unmarshal(itemRaw, &wrapper); err != nil {
			continue
		}

		data := wrapper.Item.Data
		if data.Typename != "Playlist" {
			continue
		}
		if data.URI == "" || !isSpotifyPlaylistURI(data.URI) {
			continue
		}

		playlistID := extractIDFromURI(data.URI)
		imageURL := ""
		if len(data.Images.Items) > 0 && len(data.Images.Items[0].Sources) > 0 {
			imageURL = data.Images.Items[0].Sources[0].URL
		}

		playlists = append(playlists, SpotifyGraphQLPlaylist{
			ID:         playlistID,
			Name:       data.Name,
			OwnerID:    data.OwnerV2.Data.Username,
			ImageURL:   imageURL,
			TrackCount: data.Content.TotalCount,
		})
	}

	return playlists, nil
}

// parsePlaylistTracksResponse parses the fetchPlaylist GraphQL response.
// Returns parsed tracks and the raw item count for pagination control.
func parsePlaylistTracksResponse(raw json.RawMessage) ([]SpotifyGraphQLTrack, int, error) {
	var resp struct {
		Data struct {
			PlaylistV2 struct {
				Content struct {
					Items []json.RawMessage `json:"items"`
				} `json:"content"`
			} `json:"playlistV2"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse fetchPlaylist response: %w", err)
	}

	rawItems := resp.Data.PlaylistV2.Content.Items
	rawCount := len(rawItems)
	var tracks []SpotifyGraphQLTrack

	for _, itemRaw := range rawItems {
		var item struct {
			ItemV2 struct {
				Data struct {
					Typename string `json:"__typename"`
					URI      string `json:"uri"`
					Name     string `json:"name"`
					Artists  struct {
						Items []struct {
							URI     string `json:"uri"`
							Profile struct {
								Name string `json:"name"`
							} `json:"profile"`
						} `json:"items"`
					} `json:"artists"`
					AlbumOfTrack struct {
						URI      string `json:"uri"`
						Name     string `json:"name"`
						CoverArt struct {
							Sources []struct {
								URL string `json:"url"`
							} `json:"sources"`
						} `json:"coverArt"`
					} `json:"albumOfTrack"`
				} `json:"data"`
			} `json:"itemV2"`
		}

		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue
		}

		data := item.ItemV2.Data
		if data.Typename != "TrackResponseWrapper" && data.Typename != "Track" {
			continue
		}
		if data.URI == "" || data.Name == "" {
			continue
		}

		artistName := ""
		if len(data.Artists.Items) > 0 {
			artistName = data.Artists.Items[0].Profile.Name
		}
		coverURL := ""
		if len(data.AlbumOfTrack.CoverArt.Sources) > 0 {
			coverURL = data.AlbumOfTrack.CoverArt.Sources[0].URL
		}

		tracks = append(tracks, SpotifyGraphQLTrack{
			ID:       extractIDFromURI(data.URI),
			Name:     data.Name,
			Artist:   artistName,
			Album:    data.AlbumOfTrack.Name,
			CoverURL: coverURL,
			URI:      data.URI,
		})
	}

	return tracks, rawCount, nil
}

// parseLikedSongsResponse parses the fetchLibraryTracks GraphQL response.
func parseLikedSongsResponse(raw json.RawMessage) ([]SpotifyGraphQLTrack, error) {
	var resp struct {
		Data struct {
			Me struct {
				LibraryTracks struct {
					Items []json.RawMessage `json:"items"`
				} `json:"library"`
			} `json:"me"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		// Try alternate structure
		return parseLikedSongsAlt(raw)
	}

	return parseTrackItems(resp.Data.Me.LibraryTracks.Items)
}

// parseLikedSongsAlt tries an alternate JSON structure for liked songs.
func parseLikedSongsAlt(raw json.RawMessage) ([]SpotifyGraphQLTrack, error) {
	var resp struct {
		Data struct {
			Me struct {
				TracksV2 struct {
					Items []json.RawMessage `json:"items"`
				} `json:"tracksV2"`
			} `json:"me"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse fetchLibraryTracks response: %w", err)
	}

	return parseTrackItems(resp.Data.Me.TracksV2.Items)
}

// parseTrackItems parses individual track items from GraphQL list responses.
func parseTrackItems(items []json.RawMessage) ([]SpotifyGraphQLTrack, error) {
	var tracks []SpotifyGraphQLTrack
	for _, itemRaw := range items {
		var item struct {
			Track struct {
				Data struct {
					URI  string `json:"uri"`
					Name string `json:"name"`
					Artists struct {
						Items []struct {
							URI     string `json:"uri"`
							Profile struct {
								Name string `json:"name"`
							} `json:"profile"`
						} `json:"items"`
					} `json:"artists"`
					AlbumOfTrack struct {
						Name     string `json:"name"`
						CoverArt struct {
							Sources []struct {
								URL string `json:"url"`
							} `json:"sources"`
						} `json:"coverArt"`
					} `json:"albumOfTrack"`
				} `json:"data"`
			} `json:"track"`
		}

		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue
		}

		data := item.Track.Data
		if data.URI == "" || data.Name == "" {
			continue
		}

		artistName := ""
		if len(data.Artists.Items) > 0 {
			artistName = data.Artists.Items[0].Profile.Name
		}
		coverURL := ""
		if len(data.AlbumOfTrack.CoverArt.Sources) > 0 {
			coverURL = data.AlbumOfTrack.CoverArt.Sources[0].URL
		}

		tracks = append(tracks, SpotifyGraphQLTrack{
			ID:       extractIDFromURI(data.URI),
			Name:     data.Name,
			Artist:   artistName,
			Album:    data.AlbumOfTrack.Name,
			CoverURL: coverURL,
			URI:      data.URI,
		})
	}
	return tracks, nil
}

// parseHomeFeedResponse parses the home GraphQL response to extract Spotify-generated playlists.
func parseHomeFeedResponse(raw json.RawMessage) ([]SpotifyGraphQLPlaylist, error) {
	var resp struct {
		Data struct {
			Home struct {
				SectionContainer struct {
					Sections struct {
						Items []struct {
							SectionItems struct {
								Items []json.RawMessage `json:"items"`
							} `json:"sectionItems"`
						} `json:"items"`
					} `json:"sections"`
				} `json:"sectionContainer"`
			} `json:"home"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse home feed response: %w", err)
	}

	var playlists []SpotifyGraphQLPlaylist
	for _, section := range resp.Data.Home.SectionContainer.Sections.Items {
		for _, itemRaw := range section.SectionItems.Items {
			var item struct {
				Content struct {
					Data struct {
						Typename string `json:"__typename"`
						URI      string `json:"uri"`
						Name     string `json:"name"`
						Images   struct {
							Items []struct {
								Sources []struct {
									URL string `json:"url"`
								} `json:"sources"`
							} `json:"items"`
						} `json:"images"`
					} `json:"data"`
				} `json:"content"`
			}

			if err := json.Unmarshal(itemRaw, &item); err != nil {
				continue
			}

			data := item.Content.Data
			if data.Typename != "Playlist" {
				continue
			}
			if !isSpotifyPlaylistURI(data.URI) {
				continue
			}
			if !isSpotifyMixPlaylist(data.Name) {
				continue
			}

			imageURL := ""
			if len(data.Images.Items) > 0 && len(data.Images.Items[0].Sources) > 0 {
				imageURL = data.Images.Items[0].Sources[0].URL
			}

			playlists = append(playlists, SpotifyGraphQLPlaylist{
				ID:       extractIDFromURI(data.URI),
				Name:     data.Name,
				ImageURL: imageURL,
			})
		}
	}

	return playlists, nil
}

// isSpotifyPlaylistURI checks if a URI is a Spotify playlist URI.
func isSpotifyPlaylistURI(uri string) bool {
	return len(uri) > 17 && uri[:17] == "spotify:playlist:"
}

// extractIDFromURI extracts the ID from a Spotify URI (e.g., "spotify:playlist:xxx" → "xxx").
func extractIDFromURI(uri string) string {
	parts := splitURI(uri)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return uri
}

func splitURI(uri string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(uri); i++ {
		if uri[i] == ':' {
			parts = append(parts, uri[start:i])
			start = i + 1
		}
	}
	parts = append(parts, uri[start:])
	return parts
}

// isSpotifyMixPlaylist checks if a playlist name matches known Spotify-generated mixes.
func isSpotifyMixPlaylist(name string) bool {
	lower := toLowerASCII(name)
	mixNames := []string{
		"discover weekly",
		"release radar",
		"on repeat",
		"repeat rewind",
		"time capsule",
		"daylist",
	}
	for _, m := range mixNames {
		if lower == m {
			return true
		}
	}
	// Daily Mix N pattern
	if len(name) >= 11 && toLowerASCII(name[:9]) == "daily mix" {
		return true
	}
	return false
}

func toLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

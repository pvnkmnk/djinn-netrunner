package services

import (
	"encoding/json"
	"testing"
)

func TestParseLibraryV3Response(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"me": {
				"libraryV3": {
					"items": [
						{
							"item": {
								"data": {
									"__typename": "Playlist",
									"uri": "spotify:playlist:37i9dQZF1DXcBWIGoYBM5M",
									"name": "Today's Top Hits",
									"ownerV2": {
										"data": { "username": "spotify" }
									},
									"images": {
										"items": [
											{
												"sources": [
													{ "url": "https://mosaic.scdn.co/640/image.jpg" }
												]
											}
										]
									},
									"content": { "totalCount": 50 }
								}
							}
						},
						{
							"item": {
								"data": {
									"__typename": "Album",
									"uri": "spotify:album:skip",
									"name": "Skipped Album"
								}
							}
						}
					]
				}
			}
		}
	}`)

	playlists, err := parseLibraryV3Response(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(playlists) != 1 {
		t.Fatalf("expected 1 playlist (album skipped), got %d", len(playlists))
	}
	if playlists[0].ID != "37i9dQZF1DXcBWIGoYBM5M" {
		t.Errorf("expected ID '37i9dQZF1DXcBWIGoYBM5M', got %q", playlists[0].ID)
	}
	if playlists[0].Name != "Today's Top Hits" {
		t.Errorf("expected name 'Today's Top Hits', got %q", playlists[0].Name)
	}
	if playlists[0].OwnerID != "spotify" {
		t.Errorf("expected owner 'spotify', got %q", playlists[0].OwnerID)
	}
	if playlists[0].TrackCount != 50 {
		t.Errorf("expected 50 tracks, got %d", playlists[0].TrackCount)
	}
}

func TestParsePlaylistTracksResponse(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"playlistV2": {
				"content": {
					"items": [
						{
							"itemV2": {
								"data": {
									"__typename": "Track",
									"uri": "spotify:track:abc123",
									"name": "Bohemian Rhapsody",
									"artists": {
										"items": [
											{
												"uri": "spotify:artist:xxx",
												"profile": { "name": "Queen" }
											}
										]
									},
									"albumOfTrack": {
										"uri": "spotify:album:yyy",
										"name": "A Night at the Opera",
										"coverArt": {
											"sources": [
												{ "url": "https://cover.jpg" }
											]
										}
									}
								}
							}
						},
						{
							"itemV2": {
								"data": {
									"__typename": "Episode",
									"uri": "spotify:episode:skip",
									"name": "Podcast Episode"
								}
							}
						}
					]
				}
			}
		}
	}`)

	tracks, rawCount, err := parsePlaylistTracksResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// rawCount should be 2 (both items in the array)
	if rawCount != 2 {
		t.Errorf("expected rawCount 2, got %d", rawCount)
	}
	// But only 1 track should be parsed (Episode is filtered)
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track (episode filtered), got %d", len(tracks))
	}
	if tracks[0].Name != "Bohemian Rhapsody" {
		t.Errorf("expected 'Bohemian Rhapsody', got %q", tracks[0].Name)
	}
	if tracks[0].Artist != "Queen" {
		t.Errorf("expected artist 'Queen', got %q", tracks[0].Artist)
	}
	if tracks[0].Album != "A Night at the Opera" {
		t.Errorf("expected album 'A Night at the Opera', got %q", tracks[0].Album)
	}
	if tracks[0].ID != "abc123" {
		t.Errorf("expected ID 'abc123', got %q", tracks[0].ID)
	}
}

func TestParsePlaylistTracksResponse_RawCountVsFilteredCount(t *testing.T) {
	// Simulates the pagination fix: 3 raw items but only 2 are tracks
	response := json.RawMessage(`{
		"data": {
			"playlistV2": {
				"content": {
					"items": [
						{"itemV2": {"data": {"__typename": "Track", "uri": "spotify:track:1", "name": "Track 1", "artists": {"items": []}, "albumOfTrack": {"name": "Album"}}}},
						{"itemV2": {"data": {"__typename": "LocalTrack", "uri": "", "name": "Local File"}}},
						{"itemV2": {"data": {"__typename": "Track", "uri": "spotify:track:2", "name": "Track 2", "artists": {"items": []}, "albumOfTrack": {"name": "Album"}}}}
					]
				}
			}
		}
	}`)

	tracks, rawCount, err := parsePlaylistTracksResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rawCount != 3 {
		t.Errorf("rawCount should be 3, got %d", rawCount)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks (LocalTrack filtered), got %d", len(tracks))
	}
}

func TestExtractIDFromURI(t *testing.T) {
	tests := []struct {
		uri      string
		expected string
	}{
		{"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M", "37i9dQZF1DXcBWIGoYBM5M"},
		{"spotify:track:abc123", "abc123"},
		{"spotify:album:xyz", "xyz"},
		{"plain-id", "plain-id"},
	}

	for _, tc := range tests {
		result := extractIDFromURI(tc.uri)
		if result != tc.expected {
			t.Errorf("extractIDFromURI(%q) = %q, want %q", tc.uri, result, tc.expected)
		}
	}
}

func TestIsSpotifyMixPlaylist(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Discover Weekly", true},
		{"discover weekly", true},
		{"Release Radar", true},
		{"Daily Mix 1", true},
		{"Daily Mix 6", true},
		{"On Repeat", true},
		{"Repeat Rewind", true},
		{"Time Capsule", true},
		{"Daylist", true},
		{"My Custom Playlist", false},
		{"Rock Classics", false},
	}

	for _, tc := range tests {
		result := isSpotifyMixPlaylist(tc.name)
		if result != tc.expected {
			t.Errorf("isSpotifyMixPlaylist(%q) = %v, want %v", tc.name, result, tc.expected)
		}
	}
}

func TestGraphqlTracksToMaps(t *testing.T) {
	tracks := []SpotifyGraphQLTrack{
		{ID: "1", Name: "Song A", Artist: "Artist A", Album: "Album A", CoverURL: "https://cover-a.jpg"},
		{ID: "2", Name: "Song B", Artist: "Artist B", Album: "Album B", CoverURL: ""},
	}

	result := graphqlTracksToMaps(tracks)
	if len(result) != 2 {
		t.Fatalf("expected 2 maps, got %d", len(result))
	}
	if result[0]["id"] != "1" || result[0]["artist"] != "Artist A" || result[0]["title"] != "Song A" {
		t.Errorf("first track map mismatch: %v", result[0])
	}
	if result[1]["cover_art_url"] != "" {
		t.Errorf("expected empty cover_art_url, got %q", result[1]["cover_art_url"])
	}
}

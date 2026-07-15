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

// --- parseLikedSongsResponse tests ---

func TestParseLikedSongsResponse(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"me": {
				"library": {
					"items": [
						{
							"track": {
								"data": {
									"__typename": "Track",
									"uri": "spotify:track:4uLU6hMCjMI75M1A2tKUQC",
									"name": "Hozier - Take Me to Church",
									"artists": {
										"items": [
											{
												"uri": "spotify:artist:2QLbLamancR5TvQwPOn3Uy",
												"profile": { "name": "Hozier" }
											}
										]
									},
									"albumOfTrack": {
										"name": "Take Me to Church",
										"coverArt": {
											"sources": [
												{ "url": "https://i.scdn.co/image/ab67616d0000b2738211fe2 Stack" }
											]
										}
									}
								}
							}
						},
						{
							"track": {
								"data": {
									"__typename": "Track",
									"uri": "spotify:track:1OIlgXYXvN8oW1vWgRIfoc",
									"name": "The Strokes - Reptilia",
									"artists": {
										"items": [
											{
												"uri": "spotify:artist:S OnmEC",
												"profile": { "name": "The Strokes" }
											}
										]
									},
									"albumOfTrack": {
										"name": "Room on Fire",
										"coverArt": {
											"sources": [
												{ "url": "https://i.scdn.co/image/ab67616d0000b2739 Stack" }
											]
										}
									}
								}
							}
						},
						{
							"track": {
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

	tracks, err := parseLikedSongsResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(tracks))
	}

	if tracks[0].Name != "Hozier - Take Me to Church" {
		t.Errorf("expected first track name 'Hozier - Take Me to Church', got %q", tracks[0].Name)
	}
	if tracks[0].Artist != "Hozier" {
		t.Errorf("expected first track artist 'Hozier', got %q", tracks[0].Artist)
	}
	if tracks[0].Album != "Take Me to Church" {
		t.Errorf("expected first track album 'Take Me to Church', got %q", tracks[0].Album)
	}
	if tracks[0].ID != "4uLU6hMCjMI75M1A2tKUQC" {
		t.Errorf("expected first track ID '4uLU6hMCjMI75M1A2tKUQC', got %q", tracks[0].ID)
	}

	if tracks[1].Name != "The Strokes - Reptilia" {
		t.Errorf("expected second track name 'The Strokes - Reptilia', got %q", tracks[1].Name)
	}
	if tracks[1].Artist != "The Strokes" {
		t.Errorf("expected second track artist 'The Strokes', got %q", tracks[1].Artist)
	}

	// Third track is Episode - parseTrackItems does NOT filter by typename
	if tracks[2].Name != "Podcast Episode" {
		t.Errorf("expected third track name 'Podcast Episode', got %q", tracks[2].Name)
	}
	if tracks[2].URI != "spotify:episode:skip" {
		t.Errorf("expected third track URI 'spotify:episode:skip', got %q", tracks[2].URI)
	}
	if tracks[2].Artist != "" {
		t.Errorf("expected third track artist '', got %q", tracks[2].Artist)
	}
}

func TestParseLikedSongsResponse_EmptyItems(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"me": {
				"library": {
					"items": []
				}
			}
		}
	}`)

	tracks, err := parseLikedSongsResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tracks) != 0 {
		t.Fatalf("expected 0 tracks, got %d", len(tracks))
	}
}

func TestParseLikedSongsResponse_InvalidJSON(t *testing.T) {
	response := json.RawMessage(`{invalid json}`)

	_, err := parseLikedSongsResponse(response)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- parseLikedSongsAlt tests ---

func TestParseLikedSongsAlt(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"me": {
				"tracksV2": {
					"items": [
						{
							"track": {
								"data": {
									"__typename": "Track",
									"uri": "spotify:track:abc123",
									"name": "Alt Structure Song",
									"artists": {
										"items": [
											{
												"uri": "spotify:artist:art123",
												"profile": { "name": "Alt Artist" }
											}
										]
									},
									"albumOfTrack": {
										"name": "Alt Album",
										"coverArt": {
											"sources": [
												{ "url": "https://cover.jpg" }
											]
										}
									}
								}
							}
						}
					]
				}
			}
		}
	}`)

	tracks, err := parseLikedSongsAlt(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Name != "Alt Structure Song" {
		t.Errorf("expected name 'Alt Structure Song', got %q", tracks[0].Name)
	}
	if tracks[0].Artist != "Alt Artist" {
		t.Errorf("expected artist 'Alt Artist', got %q", tracks[0].Artist)
	}
	if tracks[0].Album != "Alt Album" {
		t.Errorf("expected album 'Alt Album', got %q", tracks[0].Album)
	}
	if tracks[0].ID != "abc123" {
		t.Errorf("expected ID 'abc123', got %q", tracks[0].ID)
	}
}

func TestParseLikedSongsAlt_EmptyItems(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"me": {
				"tracksV2": {
					"items": []
				}
			}
		}
	}`)

	tracks, err := parseLikedSongsAlt(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tracks) != 0 {
		t.Fatalf("expected 0 tracks, got %d", len(tracks))
	}
}

func TestParseLikedSongsAlt_InvalidJSON(t *testing.T) {
	response := json.RawMessage(`{invalid}`)

	_, err := parseLikedSongsAlt(response)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- parseTrackItems tests ---

func TestParseTrackItems(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{
			"track": {
				"data": {
					"__typename": "Track",
					"uri": "spotify:track:track1",
					"name": "Track One",
					"artists": {
						"items": [
							{
								"uri": "spotify:artist:art1",
								"profile": { "name": "Artist One" }
							}
						]
					},
					"albumOfTrack": {
						"name": "Album One",
						"coverArt": {
							"sources": [
								{ "url": "https://cover1.jpg" }
							]
						}
					}
				}
			}
		}`),
		json.RawMessage(`{
			"track": {
				"data": {
					"__typename": "Track",
					"uri": "spotify:track:track2",
					"name": "Track Two",
					"artists": {
						"items": []
					},
					"albumOfTrack": {
						"name": "Album Two",
						"coverArt": {
							"sources": []
						}
					}
				}
			}
		}`),
		json.RawMessage(`{
			"track": {
				"data": {
					"__typename": "LocalTrack",
					"uri": "",
					"name": "Local File"
				}
			}
		}`),
	}

	tracks, err := parseTrackItems(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Local track with empty URI should be filtered out
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}

	if tracks[0].Name != "Track One" {
		t.Errorf("expected first track name 'Track One', got %q", tracks[0].Name)
	}
	if tracks[0].Artist != "Artist One" {
		t.Errorf("expected first track artist 'Artist One', got %q", tracks[0].Artist)
	}
	if tracks[0].CoverURL != "https://cover1.jpg" {
		t.Errorf("expected first track cover 'https://cover1.jpg', got %q", tracks[0].CoverURL)
	}

	// Second track has no artists - should have empty artist name
	if tracks[1].Artist != "" {
		t.Errorf("expected second track artist '', got %q", tracks[1].Artist)
	}
	if tracks[1].CoverURL != "" {
		t.Errorf("expected second track cover '', got %q", tracks[1].CoverURL)
	}
}

func TestParseTrackItems_EmptySlice(t *testing.T) {
	items := []json.RawMessage{}

	tracks, err := parseTrackItems(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tracks) != 0 {
		t.Fatalf("expected 0 tracks, got %d", len(tracks))
	}
}

func TestParseTrackItems_MalformedItem(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{invalid json}`),
		json.RawMessage(`{
			"track": {
				"data": {
					"__typename": "Track",
					"uri": "spotify:track:valid",
					"name": "Valid Track",
					"artists": { "items": [] },
					"albumOfTrack": { "name": "Album", "coverArt": { "sources": [] } }
				}
			}
		}`),
	}

	tracks, err := parseTrackItems(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed item should be skipped, valid one kept
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
}

// --- parseHomeFeedResponse tests ---

func TestParseHomeFeedResponse(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"home": {
				"sectionContainer": {
					"sections": {
						"items": [
							{
								"sectionItems": {
									"items": [
										{
											"content": {
												"data": {
													"__typename": "Playlist",
													"uri": "spotify:playlist:dailymix1",
													"name": "Daily Mix 1",
													"images": {
														"items": [
															{
																"sources": [
																	{ "url": "https://daily-mix-1.jpg" }
																]
															}
														]
													}
												}
											}
										},
										{
											"content": {
												"data": {
													"__typename": "Playlist",
													"uri": "spotify:playlist:discoverweekly",
													"name": "Discover Weekly",
													"images": {
														"items": [
															{
																"sources": [
																	{ "url": "https://discover-weekly.jpg" }
																]
															}
														]
													}
												}
											}
										},
										{
											"content": {
												"data": {
													"__typename": "Album",
													"uri": "spotify:album:skip",
													"name": "Skipped Album"
												}
											}
										},
										{
											"content": {
												"data": {
													"__typename": "Playlist",
													"uri": "spotify:playlist:userplaylist",
													"name": "My Custom Playlist",
													"images": {
														"items": [
															{
																"sources": [
																	{ "url": "https://custom.jpg" }
																]
															}
														]
													}
												}
											}
										}
									]
								}
							}
						]
					}
				}
			}
		}
	}`)

	playlists, err := parseHomeFeedResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only include Daily Mix 1 and Discover Weekly (Spotify-generated playlists)
	// User playlist "My Custom Playlist" should be filtered out
	if len(playlists) != 2 {
		t.Fatalf("expected 2 Spotify mix playlists, got %d: %v", len(playlists), playlists)
	}

	if playlists[0].Name != "Daily Mix 1" {
		t.Errorf("expected first playlist 'Daily Mix 1', got %q", playlists[0].Name)
	}
	if playlists[0].ID != "dailymix1" {
		t.Errorf("expected first playlist ID 'dailymix1', got %q", playlists[0].ID)
	}
	if playlists[0].ImageURL != "https://daily-mix-1.jpg" {
		t.Errorf("expected first playlist image 'https://daily-mix-1.jpg', got %q", playlists[0].ImageURL)
	}

	if playlists[1].Name != "Discover Weekly" {
		t.Errorf("expected second playlist 'Discover Weekly', got %q", playlists[1].Name)
	}
}

func TestParseHomeFeedResponse_EmptySections(t *testing.T) {
	response := json.RawMessage(`{
		"data": {
			"home": {
				"sectionContainer": {
					"sections": {
						"items": []
					}
				}
			}
		}
	}`)

	playlists, err := parseHomeFeedResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(playlists) != 0 {
		t.Fatalf("expected 0 playlists, got %d", len(playlists))
	}
}

func TestParseHomeFeedResponse_InvalidJSON(t *testing.T) {
	response := json.RawMessage(`{invalid}`)

	_, err := parseHomeFeedResponse(response)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestParseHomeFeedResponse_DeepNesting(t *testing.T) {
	// Test that deeply nested but valid structure is handled
	response := json.RawMessage(`{
		"data": {
			"home": {
				"sectionContainer": {
					"sections": {
						"items": [
							{
								"sectionItems": {
									"items": [
										{
											"content": {
												"data": {
													"__typename": "Playlist",
													"uri": "spotify:playlist:repeatrepeat",
													"name": "Repeat Rewind",
													"images": {
														"items": [
															{
																"sources": [
																	{ "url": "https://repeat.jpg" }
																]
															}
														]
													}
												}
											}
										}
									]
								}
							}
						]
					}
				}
			}
		}
	}`)

	playlists, err := parseHomeFeedResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(playlists) != 1 {
		t.Fatalf("expected 1 playlist, got %d", len(playlists))
	}
	if playlists[0].Name != "Repeat Rewind" {
		t.Errorf("expected playlist name 'Repeat Rewind', got %q", playlists[0].Name)
	}
}

// --- extractIDFromURI edge case tests ---

func TestExtractIDFromURI_EdgeCases(t *testing.T) {
	tests := []struct {
		uri      string
		expected string
	}{
		// Already covered cases
		{"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M", "37i9dQZF1DXcBWIGoYBM5M"},
		{"spotify:track:abc123", "abc123"},
		{"spotify:album:xyz", "xyz"},
		{"plain-id", "plain-id"},
		// Edge cases
		{"", ""},
		{":", ""},
		{"spotify:", ""},
		{"spotify:track:", ""},
		{"spotify:playlist:", ""},
		// splitURI returns ["spotify", "playlist", "multi", "colon", "id"], last is "id"
		{"spotify:playlist:multi:colon:id", "id"},
		// splitURI returns ["spotify", "track", "with", "multiple", "colons"], last is "colons"
		{"spotify:track:with:multiple:colons", "colons"},
		{":trailing:", ""},
		{"artist:album:track", "track"},
	}

	for _, tc := range tests {
		result := extractIDFromURI(tc.uri)
		if result != tc.expected {
			t.Errorf("extractIDFromURI(%q) = %q, want %q", tc.uri, result, tc.expected)
		}
	}
}

// --- splitURI tests ---

func TestSplitURI(t *testing.T) {
	tests := []struct {
		uri      string
		expected []string
	}{
		{"spotify:playlist:abc123", []string{"spotify", "playlist", "abc123"}},
		{"spotify:track:xyz", []string{"spotify", "track", "xyz"}},
		{"plain-id", []string{"plain-id"}},
		{"", []string{""}},
		// ":" splits to ["", ""] - one empty before, one empty after
		{":", []string{"", ""}},
		{"a:b:c", []string{"a", "b", "c"}},
	}

	for _, tc := range tests {
		result := splitURI(tc.uri)
		if len(result) != len(tc.expected) {
			t.Errorf("splitURI(%q) = %v, want %v", tc.uri, result, tc.expected)
			continue
		}
		for i := range result {
			if result[i] != tc.expected[i] {
				t.Errorf("splitURI(%q)[%d] = %q, want %q", tc.uri, i, result[i], tc.expected[i])
			}
		}
	}
}

// --- isSpotifyPlaylistURI edge case tests ---

func TestIsSpotifyPlaylistURI_EdgeCases(t *testing.T) {
	tests := []struct {
		uri      string
		expected bool
	}{
		{"spotify:playlist:abc123", true},
		{"spotify:playlist:", false},
		{"spotify:playlist", false},
		{"spotify:track:abc123", false},
		{"spotify:album:abc123", false},
		{"spotify:", false},
		{"", false},
		{"spotify:playlist :abc", false}, // space in prefix
		{"spotify:playlist:abc:123", true},
	}

	for _, tc := range tests {
		result := isSpotifyPlaylistURI(tc.uri)
		if result != tc.expected {
			t.Errorf("isSpotifyPlaylistURI(%q) = %v, want %v", tc.uri, result, tc.expected)
		}
	}
}

// --- toLowerASCII tests ---

func TestToLowerASCII(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"HELLO", "hello"},
		{"Hello", "hello"},
		{"HELLO WORLD", "hello world"},
		{"already lowercase", "already lowercase"},
		{"MiXeD CaSe", "mixed case"},
		{"", ""},
		{"123ABC456", "123abc456"},
		{"UPPER", "upper"},
		{"lower", "lower"},
		{" symbols !@#$ ", " symbols !@#$ "}, // symbols unchanged
	}

	for _, tc := range tests {
		result := toLowerASCII(tc.input)
		if result != tc.expected {
			t.Errorf("toLowerASCII(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// --- SpotifyGraphQLTrack and SpotifyGraphQLPlaylist field validation ---

func TestSpotifyGraphQLTrack_Fields(t *testing.T) {
	track := SpotifyGraphQLTrack{
		ID:       "test-id",
		Name:     "Test Song",
		Artist:   "Test Artist",
		Album:    "Test Album",
		CoverURL: "https://cover.jpg",
		URI:      "spotify:track:test-id",
	}

	if track.ID != "test-id" {
		t.Errorf("ID mismatch")
	}
	if track.Name != "Test Song" {
		t.Errorf("Name mismatch")
	}
	if track.Artist != "Test Artist" {
		t.Errorf("Artist mismatch")
	}
	if track.Album != "Test Album" {
		t.Errorf("Album mismatch")
	}
	if track.CoverURL != "https://cover.jpg" {
		t.Errorf("CoverURL mismatch")
	}
	if track.URI != "spotify:track:test-id" {
		t.Errorf("URI mismatch")
	}
}

func TestSpotifyGraphQLPlaylist_Fields(t *testing.T) {
	playlist := SpotifyGraphQLPlaylist{
		ID:         "playlist-id",
		Name:       "Test Playlist",
		OwnerID:    "test-owner",
		ImageURL:   "https://image.jpg",
		TrackCount: 42,
	}

	if playlist.ID != "playlist-id" {
		t.Errorf("ID mismatch")
	}
	if playlist.Name != "Test Playlist" {
		t.Errorf("Name mismatch")
	}
	if playlist.OwnerID != "test-owner" {
		t.Errorf("OwnerID mismatch")
	}
	if playlist.ImageURL != "https://image.jpg" {
		t.Errorf("ImageURL mismatch")
	}
	if playlist.TrackCount != 42 {
		t.Errorf("TrackCount mismatch")
	}
}

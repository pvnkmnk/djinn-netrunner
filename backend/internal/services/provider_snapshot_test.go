package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastFMSnapshotID_Deterministic(t *testing.T) {
	tracks := []lastFMTrack{
		{Name: "Song A"},
		{Name: "Song B"},
	}
	tracks[0].Artist.Name = "Artist A"
	tracks[1].Artist.Name = "Artist B"

	h1 := hashTrackList(tracks)
	h2 := hashTrackList(tracks)

	assert.Equal(t, h1, h2, "same track list must produce the same hash")
	assert.Len(t, h1, 16, "hash should be 16 hex chars")
}

func TestLastFMSnapshotID_ChangesOnDifferentContent(t *testing.T) {
	tracksA := []lastFMTrack{{Name: "Song A"}}
	tracksA[0].Artist.Name = "Artist A"

	tracksB := []lastFMTrack{{Name: "Song B"}}
	tracksB[0].Artist.Name = "Artist A"

	assert.NotEqual(t, hashTrackList(tracksA), hashTrackList(tracksB),
		"different track content must produce different hashes")
}

func TestLastFMSnapshotID_EmptyList(t *testing.T) {
	h := hashTrackList(nil)
	assert.Len(t, h, 16)
}

func TestLastFMSnapshotID_IntegrationViaFetch(t *testing.T) {
	resp := lastFMLovedResponse{}
	resp.LovedTracks.Attr.Total = "2"
	resp.LovedTracks.Track = []lastFMTrack{
		{Name: "Track1"},
		{Name: "Track2"},
	}
	resp.LovedTracks.Track[0].Artist.Name = "ArtistX"
	resp.LovedTracks.Track[1].Artist.Name = "ArtistY"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	p := NewLastFMProvider("key", srv.Client())
	p.BaseURL = srv.URL + "/"

	wl := &database.Watchlist{SourceType: "lastfm_loved", SourceURI: "user1"}
	_, snap, err := p.FetchTracks(context.Background(), wl)
	require.NoError(t, err)
	assert.True(t, len(snap) > len("loved:"), "snapshot should have loved: prefix + hash")
	assert.Contains(t, snap, "loved:")
	assert.Len(t, snap, len("loved:")+16, "hash portion should be 16 hex chars")
}

func TestDiscogsSnapshotID_Deterministic(t *testing.T) {
	wants := []discogsWant{
		{DateAdded: "2024-01-01"},
		{DateAdded: "2024-01-02"},
	}
	wants[0].BasicInformation.Title = "Album A"
	wants[0].BasicInformation.Artists = []struct {
		Name string `json:"name"`
	}{{Name: "Artist A"}}
	wants[1].BasicInformation.Title = "Album B"
	wants[1].BasicInformation.Artists = []struct {
		Name string `json:"name"`
	}{{Name: "Artist B"}}

	h1 := hashWantlist(wants)
	h2 := hashWantlist(wants)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 16)
}

func TestDiscogsSnapshotID_DetectsRemoval(t *testing.T) {
	wants2 := []discogsWant{
		{DateAdded: "2024-01-01"},
		{DateAdded: "2024-01-02"},
	}
	wants2[0].BasicInformation.Title = "Album A"
	wants2[0].BasicInformation.Artists = []struct {
		Name string `json:"name"`
	}{{Name: "Artist A"}}
	wants2[1].BasicInformation.Title = "Album B"
	wants2[1].BasicInformation.Artists = []struct {
		Name string `json:"name"`
	}{{Name: "Artist B"}}

	wants1 := wants2[:1]

	assert.NotEqual(t, hashWantlist(wants2), hashWantlist(wants1),
		"removing an item should change the hash")
}

func TestDiscogsSnapshotID_IntegrationViaFetch(t *testing.T) {
	resp := discogsWantlistResponse{}
	resp.Pagination.Items = 1
	resp.Wants = []discogsWant{{DateAdded: "2024-06-01"}}
	resp.Wants[0].BasicInformation.Title = "TestAlbum"
	resp.Wants[0].BasicInformation.Artists = []struct {
		Name string `json:"name"`
	}{{Name: "TestArtist"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	p := NewDiscogsProvider("token", srv.Client())
	p.BaseURL = srv.URL + "/"

	wl := &database.Watchlist{SourceType: "discogs_wantlist", SourceURI: "user1"}
	_, snap, err := p.FetchTracks(context.Background(), wl)
	require.NoError(t, err)
	assert.Contains(t, snap, "wantlist:1:")
	assert.Greater(t, len(snap), len("wantlist:1:"))
}

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastFMProvider_FetchTracks(t *testing.T) {
	// Mock Last.fm API response
	mockResponse := `{
		"lovedtracks": {
			"track": [
				{
					"name": "Test Track",
					"artist": {
						"name": "Test Artist"
					},
					"album": {
						"name": "Test Album"
					},
					"image": [
						{"#text": "http://example.com/image.png", "size": "extralarge"}
					]
				}
			],
			"@attr": {
				"total": "1"
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	provider := &LastFMProvider{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "lastfm_loved",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.Equal(t, "loved:1", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Test Track", tracks[0]["title"])
}

func TestLastFMProvider_FetchTracks_MultiPage(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		page := r.URL.Query().Get("page")
		limit := r.URL.Query().Get("limit")
		assert.Equal(t, "200", limit)

		w.Header().Set("Content-Type", "application/json")

		if page == "1" {
			tracks := make([]map[string]interface{}, 200)
			for i := range tracks {
				tracks[i] = map[string]interface{}{
					"name":   fmt.Sprintf("Track %d", i+1),
					"artist": map[string]string{"name": fmt.Sprintf("Artist %d", i+1)},
					"album":  map[string]string{"name": "Album"},
					"image":  []interface{}{},
				}
			}
			resp := map[string]interface{}{
				"lovedtracks": map[string]interface{}{
					"track": tracks,
					"@attr": map[string]string{"total": "250"},
				},
			}
			json, _ := json.Marshal(resp)
			w.Write(json)
		} else if page == "2" {
			tracks := make([]map[string]interface{}, 50)
			for i := range tracks {
				tracks[i] = map[string]interface{}{
					"name":   fmt.Sprintf("Track %d", 200+i+1),
					"artist": map[string]string{"name": fmt.Sprintf("Artist %d", 200+i+1)},
					"album":  map[string]string{"name": "Album"},
					"image":  []interface{}{},
				}
			}
			resp := map[string]interface{}{
				"lovedtracks": map[string]interface{}{
					"track": tracks,
					"@attr": map[string]string{"total": "250"},
				},
			}
			json, _ := json.Marshal(resp)
			w.Write(json)
		}
	}))
	defer server.Close()

	provider := &LastFMProvider{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "lastfm_loved",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	require.NoError(t, err)
	assert.Equal(t, "loved:250", snap)
	assert.Len(t, tracks, 250)
	assert.Equal(t, 2, requestCount)
}

func TestLastFMProvider_FetchTracks_TopWithPeriod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "overall", r.URL.Query().Get("period"))
		assert.Equal(t, "user.gettoptracks", r.URL.Query().Get("method"))

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"toptracks": {
				"track": [
					{
						"name": "Top Track",
						"artist": {"name": "Top Artist"},
						"album": {"name": "Top Album"},
						"image": []
					}
				],
				"@attr": {"total": "1"}
			}
		}`))
	}))
	defer server.Close()

	provider := &LastFMProvider{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "lastfm_top",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	require.NoError(t, err)
	assert.Equal(t, "top:1", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Top Artist", tracks[0]["artist"])
}

func TestLastFMProvider_ValidateConfig(t *testing.T) {
	provider := &LastFMProvider{}

	assert.NoError(t, provider.ValidateConfig("testuser"))
	assert.Error(t, provider.ValidateConfig(""))
}

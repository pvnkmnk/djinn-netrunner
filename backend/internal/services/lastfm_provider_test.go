package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
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
	assert.True(t, strings.HasPrefix(snap, "loved:"), "snapshot should start with loved:")
	assert.Len(t, snap, len("loved:")+16, "snapshot hash should be 16 hex chars")
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Test Track", tracks[0]["title"])
}

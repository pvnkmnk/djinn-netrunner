package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestDiscogsProvider_FetchTracks(t *testing.T) {
	// Mock Discogs Wantlist API response
	mockResponse := `{
		"pagination": {
			"items": 1
		},
		"wants": [
			{
				"basic_information": {
					"title": "Test Album",
					"artists": [
						{
							"name": "Test Artist"
						}
					],
					"cover_image": "http://example.com/cover.jpg"
				},
				"date_added": "2026-03-11T10:00:00-07:00"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	provider := &DiscogsProvider{
		Token:   "test-token",
		BaseURL: server.URL,
	}

	watchlist := &database.Watchlist{
		SourceType: "discogs_wantlist",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.Equal(t, "wantlist:2026-03-11T10:00:00-07:00", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Test Album", tracks[0]["title"])
}

package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestListenBrainzProvider_FetchTracks(t *testing.T) {
	// Mock ListenBrainz API response
	mockResponse := `{
		"payload": {
			"listens": [
				{
					"track_metadata": {
						"artist_name": "Test Artist",
						"track_name": "Test Track",
						"release_name": "Test Album",
						"additional_info": {
							"recording_msid": "test-msid"
						}
					},
					"listened_at": 1678530000
				}
			],
			"count": 1,
			"user_id": "testuser"
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	provider := &ListenBrainzProvider{
		Token:   "test-token",
		BaseURL: server.URL,
	}

	watchlist := &database.Watchlist{
		SourceType: "listenbrainz_listens",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.Equal(t, "listens:1678530000", snap)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Test Track", tracks[0]["title"])
}

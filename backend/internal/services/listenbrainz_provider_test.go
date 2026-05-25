package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Token:      "test-token",
		BaseURL:    server.URL,
		httpClient: server.Client(),
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

func TestListenBrainzProvider_FetchTracks_Pagination(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		countParam := r.URL.Query().Get("count")
		assert.Equal(t, "100", countParam)
		maxTSParam := r.URL.Query().Get("max_ts")

		w.Header().Set("Content-Type", "application/json")

		if maxTSParam == "" {
			// First page: 100 listens with timestamps 1000-1099
			listens := make([]map[string]interface{}, 100)
			for i := range listens {
				ts := int64(1099 - i)
				listens[i] = map[string]interface{}{
					"track_metadata": map[string]interface{}{
						"artist_name":  fmt.Sprintf("Artist %d", i+1),
						"track_name":   fmt.Sprintf("Track %d", i+1),
						"release_name": "Album",
					},
					"listened_at": ts,
				}
			}
			resp := map[string]interface{}{
				"payload": map[string]interface{}{
					"listens": listens,
					"count":   100,
					"user_id": "testuser",
				},
			}
			data, _ := json.Marshal(resp)
			w.Write(data)
		} else {
			ts, _ := strconv.ParseInt(maxTSParam, 10, 64)
			assert.Equal(t, int64(1000), ts)
			// Second page: 50 listens (partial page stops pagination)
			listens := make([]map[string]interface{}, 50)
			for i := range listens {
				listens[i] = map[string]interface{}{
					"track_metadata": map[string]interface{}{
						"artist_name":  fmt.Sprintf("Artist %d", 100+i+1),
						"track_name":   fmt.Sprintf("Track %d", 100+i+1),
						"release_name": "Album",
					},
					"listened_at": int64(999 - i),
				}
			}
			resp := map[string]interface{}{
				"payload": map[string]interface{}{
					"listens": listens,
					"count":   50,
					"user_id": "testuser",
				},
			}
			data, _ := json.Marshal(resp)
			w.Write(data)
		}
	}))
	defer server.Close()

	provider := &ListenBrainzProvider{
		Token:      "test-token",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "listenbrainz_listens",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	require.NoError(t, err)
	assert.Equal(t, "listens:1099", snap)
	assert.Len(t, tracks, 150)
	assert.Equal(t, 2, requestCount)
}

func TestListenBrainzProvider_ValidateConfig(t *testing.T) {
	provider := &ListenBrainzProvider{}

	assert.NoError(t, provider.ValidateConfig("testuser"))
	assert.Error(t, provider.ValidateConfig(""))
}

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	provider := &DiscogsProvider{
		Token:      "test-token",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "discogs_wantlist",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(snap, "wantlist:1:"), "snapshot should start with wantlist:<count>:")
	assert.Len(t, snap, len("wantlist:1:")+16, "snapshot hash should be 16 hex chars")
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Test Artist", tracks[0]["artist"])
	assert.Equal(t, "Test Album", tracks[0]["title"])
}

func TestDiscogsProvider_FetchTracks_MultiPage(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		page := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("per_page")
		assert.Equal(t, "100", perPage)

		w.Header().Set("Content-Type", "application/json")

		switch page {
		case "1":
			wants := make([]map[string]interface{}, 100)
			for i := range wants {
				wants[i] = map[string]interface{}{
					"basic_information": map[string]interface{}{
						"title":       fmt.Sprintf("Album %d", i+1),
						"artists":     []map[string]string{{"name": fmt.Sprintf("Artist %d", i+1)}},
						"cover_image": "http://example.com/cover.jpg",
					},
					"date_added": "2026-03-11T10:00:00-07:00",
				}
			}
			resp := map[string]interface{}{
				"pagination": map[string]int{"items": 130},
				"wants":      wants,
			}
			data, _ := json.Marshal(resp)
			_, _ = w.Write(data)
		case "2":
			wants := make([]map[string]interface{}, 30)
			for i := range wants {
				wants[i] = map[string]interface{}{
					"basic_information": map[string]interface{}{
						"title":       fmt.Sprintf("Album %d", 100+i+1),
						"artists":     []map[string]string{{"name": fmt.Sprintf("Artist %d", 100+i+1)}},
						"cover_image": "http://example.com/cover.jpg",
					},
					"date_added": "2026-03-12T10:00:00-07:00",
				}
			}
			resp := map[string]interface{}{
				"pagination": map[string]int{"items": 130},
				"wants":      wants,
			}
			data, _ := json.Marshal(resp)
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()

	provider := &DiscogsProvider{
		Token:      "test-token",
		BaseURL:    server.URL,
		httpClient: server.Client(),
	}

	watchlist := &database.Watchlist{
		SourceType: "discogs_wantlist",
		SourceURI:  "testuser",
	}

	tracks, snap, err := provider.FetchTracks(context.Background(), watchlist)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(snap, "wantlist:130:"), "snapshot should start with wantlist:<count>:")
	assert.Len(t, snap, len("wantlist:130:")+16, "snapshot hash should be 16 hex chars")
	assert.Len(t, tracks, 130)
	assert.Equal(t, 2, requestCount)
}

func TestDiscogsProvider_ValidateConfig(t *testing.T) {
	provider := &DiscogsProvider{}

	assert.NoError(t, provider.ValidateConfig("testuser"))
	assert.Error(t, provider.ValidateConfig(""))
}

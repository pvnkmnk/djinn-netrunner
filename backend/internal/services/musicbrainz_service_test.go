package services

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to http.RoundTripper for custom request handling.
type mbRoundTripFunc func(*http.Request) (*http.Response, error)

func (f mbRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestMusicBrainzService creates a MusicBrainzService with a custom transport
// and a fast rate limiter for testing.
func newTestMusicBrainzService(transport http.RoundTripper) *MusicBrainzService {
	cfg := &config.Config{
		MusicBrainzUserAgent: "NetRunnerTest/1.0.0",
	}
	return &MusicBrainzService{
		cfg:         cfg,
		baseURL:     "https://musicbrainz.org",
		httpClient:  &http.Client{Transport: transport},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
}

// mockMusicBrainzHandler creates an httptest.Server handler that simulates MusicBrainz API responses.
func mockMusicBrainzHandler(handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for required headers
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "Missing User-Agent", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			http.Error(w, "Missing Accept header", http.StatusBadRequest)
			return
		}
		handler(w, r)
	}))
}

// TestMusicBrainzService_SearchArtist tests the SearchArtist method with httptest.
func TestMusicBrainzService_SearchArtist(t *testing.T) {
	t.Run("success returns artists", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/ws/2/artist") {
				t.Errorf("expected /ws/2/artist path, got %s", r.URL.Path)
			}
			resp := map[string]interface{}{
				"artists": []map[string]string{
					{"id": "a7b4e3d2-1234-5678-9abc-def012345678", "name": "Radiohead", "sort-name": "Radiohead", "disambiguation": "English rock band"},
					{"id": "a7b4e3d2-1234-5678-9abc-def012345679", "name": "Radiohead UK", "sort-name": "Radiohead UK", "disambiguation": "tribute band"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		artists, err := svc.SearchArtist("Radiohead")
		require.NoError(t, err)
		require.Len(t, artists, 2)
		assert.Equal(t, "Radiohead", artists[0].Name)
		assert.Equal(t, "a7b4e3d2-1234-5678-9abc-def012345678", artists[0].ID)
		assert.Equal(t, "English rock band", artists[0].Disambiguation)
	})

	t.Run("success with empty results", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"artists": []interface{}{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		artists, err := svc.SearchArtist("nonexistentartist12345")
		require.NoError(t, err)
		assert.Len(t, artists, 0)
	})

	t.Run("HTTP error returns error", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		_, err := svc.SearchArtist("Radiohead")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "musicbrainz api error")
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		_, err := svc.SearchArtist("Radiohead")
		require.Error(t, err)
	})

	t.Run("rate limit headers are handled", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"artists": []interface{}{}})
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		// Should still return empty results, not error
		artists, err := svc.SearchArtist("test")
		require.NoError(t, err)
		assert.Len(t, artists, 0)
	})

	t.Run("network error returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close without responding to cause network error
		}))
		serverURL := server.URL
		server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    serverURL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		_, err := svc.SearchArtist("Radiohead")
		require.Error(t, err)
	})
}

// TestMusicBrainzService_SearchRecording tests the SearchRecording method.
func TestMusicBrainzService_SearchRecording(t *testing.T) {
	t.Run("success returns recordings", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/ws/2/recording") {
				t.Errorf("expected /ws/2/recording path, got %s", r.URL.Path)
			}
			resp := map[string]interface{}{
				"recordings": []map[string]interface{}{
					{
						"id":      "recording-id-1",
						"title":   "Killer Joe",
						"length":  245000,
						"releases": []map[string]string{{"id": "release-id-1"}},
						"artists": []map[string]string{{"name": "Benny Benassi"}},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		recordings, err := svc.SearchRecording("Killer Joe Benny Benassi")
		require.NoError(t, err)
		require.Len(t, recordings, 1)
		assert.Equal(t, "Killer Joe", recordings[0].Title)
		assert.Equal(t, "Benny Benassi", recordings[0].Artist)
		assert.Equal(t, 245000, recordings[0].Length)
		assert.Equal(t, "release-id-1", recordings[0].ReleaseID)
	})

	t.Run("empty results", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"recordings": []interface{}{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		recordings, err := svc.SearchRecording("nonexistent")
		require.NoError(t, err)
		assert.Len(t, recordings, 0)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		defer server.Close()

		svc := &MusicBrainzService{
			cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
			baseURL:    server.URL,
			httpClient: &http.Client{Timeout: 10 * time.Second},
			rateLimiter: time.NewTicker(time.Nanosecond),
		}
		defer svc.Close()

		_, err := svc.SearchRecording("test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "musicbrainz api error")
	})
}

// TestMusicBrainzService_GetArtistDiscography tests GetArtistDiscography using a custom transport.
func TestMusicBrainzService_GetArtistDiscography(t *testing.T) {
	// Note: GetArtistDiscography uses doRequest which has a hardcoded baseURL.
	// We need to use a custom RoundTripper to intercept requests.

	t.Run("success returns discography", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.Contains(req.URL.Path, "/ws/2/artist/") {
				t.Errorf("expected /ws/2/artist/ path, got %s", req.URL.Path)
			}
			resp := map[string]interface{}{
				"id":    "artist-mbid",
				"name":  "Radiohead",
				"release-groups": []map[string]interface{}{
					{"id": "rg-1", "type": "Album", "title": "OK Computer"},
					{"id": "rg-2", "type": "Album", "title": "Kid A"},
				},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		result, err := svc.GetArtistDiscography("artist-mbid")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Radiohead", result["name"])

		// Check release-groups were parsed
		releaseGroups, ok := result["release-groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, releaseGroups, 2)
	})

	t.Run("HTTP error", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		_, err := svc.GetArtistDiscography("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "musicbrainz api error")
	})
}

// jsonStr helper for creating JSON strings
func jsonStr(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestMusicBrainzService_GetRelease tests GetRelease using a custom transport.
func TestMusicBrainzService_GetRelease(t *testing.T) {
	t.Run("success returns release details", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.Contains(req.URL.Path, "/ws/2/release/") {
				t.Errorf("expected /ws/2/release/ path, got %s", req.URL.Path)
			}
			resp := map[string]interface{}{
				"id":       "release-mbid",
				"title":    "OK Computer",
				"date":     "1997-06-16",
				"country":  "GB",
				"genres":   []map[string]string{{"name": "Rock"}},
				"media":    []map[string]interface{}{{"format": "CD", "track-count": 12}},
				"images":   []map[string]interface{}{{"image": "cover.jpg", "front": true}},
				"artist-credit": []map[string]interface{}{
					{"name": "Radiohead", "artist": map[string]string{"id": "artist-mbid", "name": "Radiohead"}},
				},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		release, err := svc.GetRelease("release-mbid")
		require.NoError(t, err)
		require.NotNil(t, release)
		assert.Equal(t, "OK Computer", release.Title)
		assert.Equal(t, "1997-06-16", release.Date)
		assert.Equal(t, "GB", release.Country)
		assert.Len(t, release.Genres, 1)
		assert.Len(t, release.Media, 1)
		assert.Len(t, release.Images, 1)
	})

	t.Run("HTTP error", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		_, err := svc.GetRelease("release-mbid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "musicbrainz api error")
	})
}

// TestMusicBrainzService_GetReleaseByArtistTitle tests GetReleaseByArtistTitle.
func TestMusicBrainzService_GetReleaseByArtistTitle(t *testing.T) {
	t.Run("success finds and returns release", func(t *testing.T) {
		requestCount := 0
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			if strings.Contains(req.URL.Path, "/ws/2/release/") {
				// Release detail request
				resp := map[string]interface{}{
					"id":    "release-mbid",
					"title": "OK Computer",
					"date":  "1997",
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
				}, nil
			}
			// Search request
			resp := map[string]interface{}{
				"releases": []map[string]string{{"id": "release-mbid"}},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		release, err := svc.GetReleaseByArtistTitle("Radiohead", "OK Computer")
		require.NoError(t, err)
		require.NotNil(t, release)
		assert.Equal(t, "OK Computer", release.Title)
		assert.Equal(t, 2, requestCount) // Search + detail
	})

	t.Run("no release found", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := map[string]interface{}{
				"releases": []interface{}{},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		_, err := svc.GetReleaseByArtistTitle("Unknown", "Unknown")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no release found")
	})
}

// TestMusicBrainzService_GetCoverArt tests GetCoverArt.
func TestMusicBrainzService_GetCoverArt(t *testing.T) {
	t.Run("success returns front cover URL", func(t *testing.T) {
		requestCount := 0
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			if strings.Contains(req.URL.Path, "/ws/2/release/") {
				resp := map[string]interface{}{
					"id":     "release-mbid",
					"title":  "OK Computer",
					"images": []map[string]interface{}{
						{"image": "back.jpg", "front": false},
						{"image": "front.jpg", "front": true},
					},
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
				}, nil
			}
			resp := map[string]interface{}{
				"releases": []map[string]string{{"id": "release-mbid"}},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		coverURL, err := svc.GetCoverArt("release-mbid")
		require.NoError(t, err)
		assert.Equal(t, "front.jpg", coverURL)
		assert.Equal(t, 1, requestCount)
	})

	t.Run("returns first image when no front cover", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := map[string]interface{}{
				"id":     "release-mbid",
				"title":  "OK Computer",
				"images": []map[string]string{{"image": "only-image.jpg"}},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		coverURL, err := svc.GetCoverArt("release-mbid")
		require.NoError(t, err)
		assert.Equal(t, "only-image.jpg", coverURL)
	})

	t.Run("no cover art found", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := map[string]interface{}{
				"id":     "release-mbid",
				"title":  "OK Computer",
				"images": []interface{}{},
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		_, err := svc.GetCoverArt("release-mbid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no cover art found")
	})

	t.Run("release lookup error", func(t *testing.T) {
		transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})

		svc := newTestMusicBrainzService(transport)
		defer svc.Close()

		_, err := svc.GetCoverArt("nonexistent")
		require.Error(t, err)
	})
}

// TestMusicBrainzService_HealthCheck tests HealthCheck.
func TestMusicBrainzService_HealthCheck(t *testing.T) {
	t.Run("returns true always", func(t *testing.T) {
		svc := NewMusicBrainzService(&config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"})
		defer svc.Close()
		assert.True(t, svc.HealthCheck())
	})
}

// TestMusicBrainzService_NewMusicBrainzService tests the constructor.
func TestMusicBrainzService_NewMusicBrainzService(t *testing.T) {
	cfg := &config.Config{
		MusicBrainzUserAgent: "NetRunnerTest/1.0.0",
	}
	svc := NewMusicBrainzService(cfg)
	require.NotNil(t, svc)
	assert.Equal(t, "https://musicbrainz.org", svc.baseURL)
	assert.NotNil(t, svc.httpClient)
	assert.NotNil(t, svc.rateLimiter)
	svc.Close()
}

// TestMusicBrainzService_SetCache tests SetCache.
func TestMusicBrainzService_SetCache(t *testing.T) {
	svc := NewMusicBrainzService(&config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"})
	defer svc.Close()

	// Should not panic
	svc.SetCache(nil)
}

// Test that malformed JSON in release response is handled
func TestMusicBrainzService_GetRelease_MalformedJSON(t *testing.T) {
	transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{invalid`)),
		}, nil
	})

	svc := newTestMusicBrainzService(transport)
	defer svc.Close()

	_, err := svc.GetRelease("release-mbid")
	require.Error(t, err)
	// Should be a JSON syntax error
	var syntaxErr *json.SyntaxError
	assert.True(t, errors.As(err, &syntaxErr) || strings.Contains(err.Error(), "invalid"))
}

// Test that HTTP 500 error is properly propagated
func TestMusicBrainzService_SearchRecording_HTTPError(t *testing.T) {
	transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})

	svc := newTestMusicBrainzService(transport)
	defer svc.Close()

	_, err := svc.SearchRecording("test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "musicbrainz api error")
}

// Test with custom User-Agent from config
func TestMusicBrainzService_CustomUserAgent(t *testing.T) {
	var receivedUserAgent string
	server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		resp := map[string]interface{}{"artists": []interface{}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	customUA := "CustomAgent/2.0 (test@example.com)"
	svc := &MusicBrainzService{
		cfg:        &config.Config{MusicBrainzUserAgent: customUA},
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer svc.Close()

	_, _ = svc.SearchArtist("test")
	assert.Equal(t, customUA, receivedUserAgent)
}

// Test doRequest includes correct headers
func TestMusicBrainzService_doRequest_Headers(t *testing.T) {
	var capturedReq *http.Request
	transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})

	svc := newTestMusicBrainzService(transport)
	defer svc.Close()

	// Access doRequest indirectly through GetArtistDiscography
	_, _ = svc.GetArtistDiscography("test-artist-id")

	require.NotNil(t, capturedReq)
	assert.Equal(t, "NetRunnerTest/1.0.0", capturedReq.Header.Get("User-Agent"))
	assert.Equal(t, "application/json", capturedReq.Header.Get("Accept"))
}

// Test that Close stops the rate limiter without error
func TestMusicBrainzService_Close(t *testing.T) {
	svc := NewMusicBrainzService(&config.Config{MusicBrainzUserAgent: "Test/1.0"})
	// Should not panic
	svc.Close()
}

// Test parsing response with missing optional fields
func TestMusicBrainzService_SearchArtist_MissingFields(t *testing.T) {
	server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
		// Response with missing optional fields
		resp := map[string]interface{}{
			"artists": []map[string]interface{}{
				{"id": "123", "name": "Test Artist"}, // missing sort-name, disambiguation
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	svc := &MusicBrainzService{
		cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer svc.Close()

	artists, err := svc.SearchArtist("test")
	require.NoError(t, err)
	require.Len(t, artists, 1)
	assert.Equal(t, "123", artists[0].ID)
	assert.Equal(t, "Test Artist", artists[0].Name)
	assert.Equal(t, "", artists[0].SortName)
	assert.Equal(t, "", artists[0].Disambiguation)
}

// Test network error on GetArtistDiscography
func TestMusicBrainzService_GetArtistDiscography_NetworkError(t *testing.T) {
	transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network error")
	})

	svc := newTestMusicBrainzService(transport)
	defer svc.Close()

	_, err := svc.GetArtistDiscography("test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
}

// Test connection error propagation
func TestMusicBrainzService_SearchArtist_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close immediately
	}))
	serverURL := server.URL
	server.Close()

	svc := &MusicBrainzService{
		cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		baseURL:    serverURL,
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer svc.Close()

	_, err := svc.SearchArtist("test")
	require.Error(t, err)
}

// Test GetReleaseByArtistTitle with invalid MBID in response
func TestMusicBrainzService_GetReleaseByArtistTitle_InvalidMBID(t *testing.T) {
	transport := mbRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Response where release id is not a string
		resp := map[string]interface{}{
			"releases": []map[string]interface{}{
				{"id": 123}, // id should be string
			},
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(jsonStr(resp))),
		}, nil
	})

	svc := newTestMusicBrainzService(transport)
	defer svc.Close()

	_, err := svc.GetReleaseByArtistTitle("test", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "release has no MBID")
}

// Test recording search with no releases attached
func TestMusicBrainzService_SearchRecording_NoReleases(t *testing.T) {
	server := mockMusicBrainzHandler(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"recordings": []map[string]interface{}{
				{"id": "rec1", "title": "Test Song", "releases": []interface{}{}}, // no releases
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	svc := &MusicBrainzService{
		cfg:        &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer svc.Close()

	recordings, err := svc.SearchRecording("test")
	require.NoError(t, err)
	require.Len(t, recordings, 1)
	assert.Equal(t, "", recordings[0].ReleaseID) // empty when no releases
}

package services

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMusicBrainzService(t *testing.T) {
	cfg := &config.Config{
		MusicBrainzUserAgent: "NetRunnerTest/1.0.0",
	}
	s := NewMusicBrainzService(cfg)
	defer s.Close()

	if s == nil {
		t.Fatal("Expected MusicBrainzService to be initialized")
	}
}

func TestSearchArtistByName(t *testing.T) {
	// Integration test - requires network access to MusicBrainz API
	// Skip in CI or when network is unavailable
	t.Skip("Integration test - requires network access to MusicBrainz")

	cfg := &config.Config{
		MusicBrainzUserAgent: "NetRunnerTest/1.0.0",
	}
	mb := NewMusicBrainzService(cfg)
	defer mb.Close()

	results, err := mb.SearchArtist("Radiohead")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)
	assert.Equal(t, "Radiohead", results[0].Name)
	assert.NotEmpty(t, results[0].ID) // MBID
}

func TestGetArtistDiscographyUsesArtistEndpoint(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	mb := &MusicBrainzService{
		cfg: &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedPath = req.URL.Path
			requestedQuery = req.URL.RawQuery
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"release-groups":[]}`)),
			}, nil
		})},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer mb.Close()

	_, err := mb.GetArtistDiscography("artist-mbid")

	require.NoError(t, err)
	assert.Equal(t, "/ws/2/artist/artist-mbid", requestedPath)
	assert.Contains(t, requestedQuery, "inc=release-groups")
	assert.NotContains(t, requestedQuery, "artist=artist-mbid")
}

func TestGetReleaseByArtistTitleUsesReleaseEndpoints(t *testing.T) {
	var requestedPaths []string

	mb := &MusicBrainzService{
		cfg: &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedPaths = append(requestedPaths, req.URL.Path)
			body := `{"releases":[{"id":"release-mbid"}]}`
			if strings.Contains(req.URL.Path, "/release/release-mbid") {
				body = `{"id":"release-mbid","title":"Test Album"}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer mb.Close()

	release, err := mb.GetReleaseByArtistTitle("Test Artist", "Test Album")

	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, []string{"/ws/2/release", "/ws/2/release/release-mbid"}, requestedPaths)
}

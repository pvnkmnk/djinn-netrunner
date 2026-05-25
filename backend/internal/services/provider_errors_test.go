package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantKind ProviderErrorKind
	}{
		{"401 auth", 401, ErrKindAuth},
		{"403 auth", 403, ErrKindAuth},
		{"429 rate limit", 429, ErrKindRateLimit},
		{"400 config", 400, ErrKindConfig},
		{"404 config", 404, ErrKindConfig},
		{"422 config", 422, ErrKindConfig},
		{"500 upstream", 500, ErrKindUpstream},
		{"502 upstream", 502, ErrKindUpstream},
		{"503 upstream", 503, ErrKindUpstream},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyHTTPStatus(tt.code, "test")
			assert.Equal(t, tt.wantKind, err.Kind)
			assert.Contains(t, err.Error(), "test api returned status")
		})
	}
}

func TestClassifyNetworkError(t *testing.T) {
	t.Run("timeout error", func(t *testing.T) {
		err := classifyNetworkError(&net.OpError{Op: "dial", Err: errors.New("i/o timeout")}, "test")
		assert.Equal(t, ErrKindNetwork, err.Kind)
	})

	t.Run("dns error", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "no such host", Name: "example.com"}
		err := classifyNetworkError(dnsErr, "test")
		assert.Equal(t, ErrKindNetwork, err.Kind)
	})

	t.Run("generic error", func(t *testing.T) {
		err := classifyNetworkError(errors.New("something broke"), "test")
		assert.Equal(t, ErrKindNetwork, err.Kind)
	})
}

func TestProviderError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	pe := NewProviderError(ErrKindAuth, "auth failed", cause)
	assert.True(t, errors.Is(pe, cause))
	assert.Equal(t, "auth failed: root cause", pe.Error())
}

func TestProviderError_NilCause(t *testing.T) {
	pe := NewProviderError(ErrKindRateLimit, "slow down", nil)
	assert.Equal(t, "slow down", pe.Error())
	assert.Nil(t, pe.Unwrap())
}

func TestLastFMProvider_ErrorClassification(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantKind ProviderErrorKind
	}{
		{"401", 401, ErrKindAuth},
		{"403", 403, ErrKindAuth},
		{"429", 429, ErrKindRateLimit},
		{"500", 500, ErrKindUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			p := NewLastFMProvider("key", srv.Client())
			p.BaseURL = srv.URL + "/"
			wl := &database.Watchlist{SourceType: "lastfm_loved", SourceURI: "u"}
			_, _, err := p.FetchTracks(context.Background(), wl)
			require.Error(t, err)
			var pe *ProviderError
			require.True(t, errors.As(err, &pe))
			assert.Equal(t, tt.wantKind, pe.Kind)
		})
	}
}

func TestDiscogsProvider_ErrorClassification(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantKind ProviderErrorKind
	}{
		{"401", 401, ErrKindAuth},
		{"429", 429, ErrKindRateLimit},
		{"500", 500, ErrKindUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			p := NewDiscogsProvider("tok", srv.Client())
			p.BaseURL = srv.URL + "/"
			wl := &database.Watchlist{SourceType: "discogs_wantlist", SourceURI: "u"}
			_, _, err := p.FetchTracks(context.Background(), wl)
			require.Error(t, err)
			var pe *ProviderError
			require.True(t, errors.As(err, &pe))
			assert.Equal(t, tt.wantKind, pe.Kind)
		})
	}
}

func TestListenBrainzProvider_ErrorClassification(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantKind ProviderErrorKind
	}{
		{"403", 403, ErrKindAuth},
		{"429", 429, ErrKindRateLimit},
		{"502", 502, ErrKindUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			p := NewListenBrainzProvider("tok", srv.Client())
			p.BaseURL = srv.URL + "/"
			wl := &database.Watchlist{SourceType: "listenbrainz_listens", SourceURI: "u"}
			_, _, err := p.FetchTracks(context.Background(), wl)
			require.Error(t, err)
			var pe *ProviderError
			require.True(t, errors.As(err, &pe))
			assert.Equal(t, tt.wantKind, pe.Kind)
		})
	}
}

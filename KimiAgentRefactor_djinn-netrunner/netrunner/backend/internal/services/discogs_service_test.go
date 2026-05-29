package services

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

// discogsMockSearchResponse mirrors the Discogs /database/search response structure.
type discogsMockSearchResponse struct {
	Pagination discogsMockPagination `json:"pagination"`
	Results    []discogsMockItem     `json:"results"`
}

type discogsMockPagination struct {
	Page    int `json:"page"`
	Pages   int `json:"pages"`
	PerPage int `json:"per_page"`
	Items   int `json:"items"`
}

type discogsMockItem struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Year        string   `json:"year"`
	Genre       []string `json:"genre"`
	CoverImage  string   `json:"cover_image"`
	Thumb       string   `json:"thumb"`
	Format      []string `json:"format"`
	Country     string   `json:"country"`
	Label       []string `json:"label"`
	ResourceURL string   `json:"resource_url"`
}

func TestDiscogsService_GetCoverArt(t *testing.T) {
	t.Run("success with cover image", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Authorization"), "Discogs") {
				t.Errorf("expected Discogs Authorization header, got: %s", r.Header.Get("Authorization"))
			}
			if r.URL.Path != "/database/search" {
				t.Errorf("expected path /database/search, got %s", r.URL.Path)
			}
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Page: 1, Pages: 1, PerPage: 10, Items: 1},
				Results: []discogsMockItem{
					{ID: 123, Title: "OK Computer", CoverImage: "https://api.discogs.com/images/release-123.jpg"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		url, err := svc.GetCoverArt("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://api.discogs.com/images/release-123.jpg" {
			t.Errorf("expected cover image URL, got %s", url)
		}
	})

	t.Run("no results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{Pagination: discogsMockPagination{Items: 0}, Results: []discogsMockItem{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.GetCoverArt("nonexistent", "notfound")
		if err == nil {
			t.Error("expected error for no results")
		}
		if !strings.Contains(err.Error(), "no release found") {
			t.Errorf("expected 'no release found' error, got: %v", err)
		}
	})

	t.Run("server returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.GetCoverArt("Radiohead", "OK Computer")
		if err == nil {
			t.Error("expected error for server error")
		}
		if !strings.Contains(err.Error(), "discogs API error") {
			t.Errorf("expected 'discogs API error' in message, got: %v", err)
		}
	})

	t.Run("search returns results but none have cover images — fallback to first result", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// All results have empty CoverImage; should fall back to first result's empty CoverImage
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 2},
				Results: []discogsMockItem{
					{ID: 1, Title: "OK Computer", CoverImage: ""},
					{ID: 2, Title: "OK Computer (Remastered)", CoverImage: ""},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		url, err := svc.GetCoverArt("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "" {
			t.Errorf("expected empty cover URL (fallback), got %s", url)
		}
	})

	t.Run("search returns results with one having cover image", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 3},
				Results: []discogsMockItem{
					{ID: 1, Title: "OK Computer", CoverImage: ""},
					{ID: 2, Title: "OK Computer (Deluxe)", CoverImage: "https://api.discogs.com/images/release-456.jpg"},
					{ID: 3, Title: "OK Computer (Remastered)", CoverImage: ""},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		url, err := svc.GetCoverArt("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://api.discogs.com/images/release-456.jpg" {
			t.Errorf("expected cover image from second result, got %s", url)
		}
	})
}

func TestDiscogsService_SearchRelease(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Authorization"), "Discogs") {
				t.Errorf("expected Discogs Authorization header, got: %s", r.Header.Get("Authorization"))
			}
			if !strings.Contains(r.Header.Get("User-Agent"), "DjinnNetRunner") {
				t.Errorf("expected User-Agent header, got: %s", r.Header.Get("User-Agent"))
			}
			if r.URL.Path != "/database/search" {
				t.Errorf("expected path /database/search, got %s", r.URL.Path)
			}
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Page: 1, Pages: 1, PerPage: 10, Items: 2},
				Results: []discogsMockItem{
					{ID: 123, Title: "OK Computer", Year: "1997", Genre: []string{"Rock"}, CoverImage: "https://api.discogs.com/images/release-123.jpg"},
					{ID: 456, Title: "OK Computer (Deluxe)", Year: "1997", Genre: []string{"Rock"}, CoverImage: "https://api.discogs.com/images/release-456.jpg"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		result, err := svc.SearchRelease("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Results) != 2 {
			t.Errorf("expected 2 results, got %d", len(result.Results))
		}
		if result.Pagination.Items != 2 {
			t.Errorf("expected 2 total items, got %d", result.Pagination.Items)
		}
	})

	t.Run("rate limited returns 429", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"message": "Rate limit exceeded"})
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.SearchRelease("Radiohead", "OK Computer")
		if err == nil {
			t.Error("expected error for rate limit")
		}
		if !strings.Contains(err.Error(), "discogs API error: 429") {
			t.Errorf("expected 'discogs API error: 429', got: %v", err)
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.SearchRelease("Radiohead", "OK Computer")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately without responding — causes a connection reset / network error
			return
		}))
		serverURL := server.URL
		server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, serverURL)
		_, err := svc.SearchRelease("Radiohead", "OK Computer")
		if err == nil {
			t.Error("expected error for network failure")
		}
		// Error should NOT be a Discogs API error (status code), but a transport error
		if strings.Contains(err.Error(), "discogs API error") {
			t.Errorf("expected transport error, got: %v", err)
		}
	})
}

// TestDiscogsService_GetGenre tests the GetGenre helper.
func TestDiscogsService_GetGenre(t *testing.T) {
	t.Run("success with genre", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 1},
				Results: []discogsMockItem{
					{ID: 123, Title: "OK Computer", Genre: []string{"Rock", "Alternative"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		genre, err := svc.GetGenre("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if genre != "Rock" {
			t.Errorf("expected 'Rock', got %s", genre)
		}
	})

	t.Run("no results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{Pagination: discogsMockPagination{Items: 0}, Results: []discogsMockItem{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.GetGenre("nonexistent", "notfound")
		if err == nil {
			t.Error("expected error for no results")
		}
		if !strings.Contains(err.Error(), "no release found") {
			t.Errorf("expected 'no release found' error, got: %v", err)
		}
	})
}

// TestDiscogsService_GetYear tests the GetYear helper.
func TestDiscogsService_GetYear(t *testing.T) {
	t.Run("success with year", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 1},
				Results: []discogsMockItem{
					{ID: 123, Title: "OK Computer", Year: "1997"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		year, err := svc.GetYear("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if year != 1997 {
			t.Errorf("expected 1997, got %d", year)
		}
	})

	t.Run("year in range format", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 1},
				Results: []discogsMockItem{
					{ID: 123, Title: "The Wall", Year: "1979, 1980"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		year, err := svc.GetYear("Pink Floyd", "The Wall")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if year != 1979 {
			t.Errorf("expected 1979 (first year in range), got %d", year)
		}
	})

	t.Run("empty year string", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 1},
				Results: []discogsMockItem{
					{ID: 123, Title: "Unknown Release", Year: ""},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		year, err := svc.GetYear("Unknown", "Unknown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if year != 0 {
			t.Errorf("expected 0 for empty year string, got %d", year)
		}
	})
}

// TestDiscogsService_EnrichTrack tests the EnrichTrack helper.
func TestDiscogsService_EnrichTrack(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{
				Pagination: discogsMockPagination{Items: 1},
				Results: []discogsMockItem{
					{
						ID:         123,
						Title:      "OK Computer",
						Year:       "1997",
						Genre:      []string{"Rock"},
						CoverImage: "https://api.discogs.com/images/release-123.jpg",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		enriched, err := svc.EnrichTrack("Radiohead", "OK Computer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if enriched["cover_url"] != "https://api.discogs.com/images/release-123.jpg" {
			t.Errorf("expected cover_url, got %v", enriched["cover_url"])
		}
		if enriched["genre"] != "Rock" {
			t.Errorf("expected genre 'Rock', got %v", enriched["genre"])
		}
		if enriched["year"] != 1997 {
			t.Errorf("expected year 1997, got %v", enriched["year"])
		}
	})

	t.Run("no results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := discogsMockSearchResponse{Pagination: discogsMockPagination{Items: 0}, Results: []discogsMockItem{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"}, server.URL)
		_, err := svc.EnrichTrack("nonexistent", "notfound")
		if err == nil {
			t.Error("expected error for no results")
		}
		var netErr error
		if errors.Is(err, netErr) {
			// ignore
		}
		_ = netErr
	})
}

// Test that NewDiscogsService uses the default base URL when no override is passed.
func TestDiscogsService_DefaultBaseURL(t *testing.T) {
	// When no baseURL override is given, the service should still function
	// with the default Discogs API. We can't hit the real API in unit tests,
	// but we can verify the constructor doesn't panic and the field is set.
	svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"})
	if svc.baseURL != "https://api.discogs.com" {
		t.Errorf("expected default baseURL 'https://api.discogs.com', got %s", svc.baseURL)
	}
}

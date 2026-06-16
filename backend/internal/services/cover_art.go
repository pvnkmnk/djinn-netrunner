package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
)

// Default cover art source priority order
var defaultCoverArtSources = []string{"source", "musicbrainz", "discogs"}

const (
	coverArtCacheTTL = 168 * time.Hour // 1 week, same as MusicBrainz cache TTL
	maxCoverSize     = 10 << 20        // 10 MB
)

// coverArtCacheKey generates a cache key for cover art lookups.
func coverArtCacheKey(artist, album, source string) string {
	return fmt.Sprintf("%s:%s:%s", strings.ToLower(artist), strings.ToLower(album), source)
}

// parseCoverArtSources splits a comma-separated CoverArtSources string into a slice.
// Returns nil (uses default) if the input is empty or invalid.
func parseCoverArtSources(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	sources := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			sources = append(sources, p)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return sources
}

// getCoverArtWithFallback attempts to fetch cover art from multiple sources in priority order
func (h *AcquisitionHandler) getCoverArtWithFallback(ctx context.Context, item *database.JobItem, artist, title, album string, sources []string) ([]byte, error) {
	if sources == nil {
		sources = defaultCoverArtSources
	}

	for _, source := range sources {
		// Check cache first
		if h.cache != nil {
			key := coverArtCacheKey(artist, album, source)
			if data, found, err := h.cache.GetBytes("coverart", key); err == nil && found {
				metrics.CoverArtFetchTotal.WithLabelValues(source, "hit_cache").Inc()
				h.Log(item.JobID, "DEBUG", fmt.Sprintf("Cover art cache hit for %s", key), &item.ID)
				return data, nil
			}
		}

		var artData []byte
		var err error

		switch source {
		case "source":
			artData, err = h.fetchCoverFromSourceURL(ctx, item)
		case "musicbrainz":
			artData, err = h.fetchCoverFromMusicBrainz(ctx, item, artist, album)
		case "discogs":
			artData, err = h.fetchCoverFromDiscogs(ctx, item, artist, title)
		}

		if err != nil {
			metrics.CoverArtFetchTotal.WithLabelValues(source, "error").Inc()
		} else if len(artData) == 0 {
			metrics.CoverArtFetchTotal.WithLabelValues(source, "empty").Inc()
		} else {
			metrics.CoverArtFetchTotal.WithLabelValues(source, "ok").Inc()
			if h.cache != nil {
				key := coverArtCacheKey(artist, album, source)
				_ = h.cache.SetBytes("coverart", key, artData, coverArtCacheTTL)
			}
			return artData, nil
		}
	}

	metrics.CoverArtFetchTotal.WithLabelValues("none", "exhausted").Inc()
	return nil, fmt.Errorf("no cover art found from any source")
}

// fetchCoverFromSourceURL extracts the source URL logic from the original function
func (h *AcquisitionHandler) fetchCoverFromSourceURL(ctx context.Context, item *database.JobItem) ([]byte, error) {
	if item.CoverArtURL == "" {
		return nil, fmt.Errorf("no source URL")
	}
	resp, err := SafeGet(item.CoverArtURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source URL returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCoverSize {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("cover art from source URL exceeds %d bytes", maxCoverSize)
	}
	return data, nil
}

// fetchCoverFromMusicBrainz extracts the MusicBrainz logic
func (h *AcquisitionHandler) fetchCoverFromMusicBrainz(ctx context.Context, item *database.JobItem, artist, album string) ([]byte, error) {
	if h.mb == nil || (artist == "" && album == "") {
		return nil, fmt.Errorf("no artist/album for MB lookup")
	}
	queryArtist := artist
	if queryArtist == "" && item.Artist != "" {
		queryArtist = item.Artist
	}
	queryAlbum := album
	if queryAlbum == "" && item.Album != "" {
		queryAlbum = item.Album
	}

	release, err := h.mb.GetReleaseByArtistTitle(queryArtist, queryAlbum)
	if err != nil || release == nil {
		return nil, err
	}
	for _, img := range release.Images {
		if img.Front {
			resp, err := SafeGet(img.Image)
			if err != nil {
				continue
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusOK {
				data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverSize+1))
				if err == nil && len(data) > maxCoverSize {
					continue
				}
				if err == nil && len(data) > 0 {
					h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from MusicBrainz: %s", img.Image), &item.ID)
					return data, nil
				}
			}
		}
	}
	// Fall back to first image
	if len(release.Images) > 0 {
		resp, err := SafeGet(release.Images[0].Image)
		if err != nil {
			return nil, fmt.Errorf("no front cover from MB")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverSize+1))
		if err == nil && len(data) > maxCoverSize {
			return nil, fmt.Errorf("cover art from MusicBrainz exceeds %d bytes", maxCoverSize)
		}
		if err == nil && len(data) > 0 {
				h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from MusicBrainz: %s", release.Images[0].Image), &item.ID)
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("no front cover from MB")
}

// fetchCoverFromDiscogs extracts the Discogs logic
func (h *AcquisitionHandler) fetchCoverFromDiscogs(ctx context.Context, item *database.JobItem, artist, title string) ([]byte, error) {
	if h.discogs == nil || artist == "" {
		return nil, fmt.Errorf("no artist for Discogs lookup")
	}
	coverURL, err := h.discogs.GetCoverArt(artist, title)
	if err != nil || coverURL == "" {
		return nil, err
	}
		resp, err := SafeGet(coverURL)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discogs returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCoverSize {
		return nil, fmt.Errorf("cover art from Discogs exceeds %d bytes", maxCoverSize)
	}
	h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from Discogs: %s", coverURL), &item.ID)
	return data, nil
}

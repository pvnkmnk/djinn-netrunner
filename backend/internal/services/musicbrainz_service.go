package services

import (
	"encoding/json"
	"errors"
	"fmt"

	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
)

// MusicBrainzService handles interaction with the MusicBrainz API
type MusicBrainzService struct {
	cfg         *config.Config
	baseURL     string
	httpClient  *http.Client
	rateLimiter *time.Ticker
	cache       *CacheService

	// TestSearch and TestGetArtist replace the two network calls this
	// service makes. Both are nil in production, where the real methods run;
	// a test sets them so the suite never touches MusicBrainz and can make
	// the search fail on demand. A search that can only succeed is a search
	// whose failure path is never exercised.
	TestSearch    func(query string) ([]MusicBrainzArtist, error)
	TestGetArtist func(id string) (*MusicBrainzArtist, error)
}

// NewMusicBrainzService creates a new MusicBrainz service
// musicBrainzBaseURL is the real MusicBrainz unless MUSICBRAINZ_URL overrides
// it.
//
// The override exists for the e2e stack, which serves MusicBrainz from a local
// stand-in (ops/fake-musicbrainz). Without it, artist-picker.spec.ts was a
// canary for musicbrainz.org's uptime and a third-party outage produced a red CI
// run on an unrelated commit -- and the gate could not express the skip, because
// scripts/e2e_gate.sh holds DECLARED_SKIPS exact in BOTH directions.
//
// It is a plain host override rather than a test hook, so it cannot be set by
// accident in production: it defaults to empty, and nothing reads it except this.
func musicBrainzBaseURL(cfg *config.Config) string {
	if cfg != nil {
		if u := strings.TrimSpace(cfg.MusicBrainzURL); u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return defaultMusicBrainzURL
}

// defaultMusicBrainzURL is the public service. Named so the override above has
// one owner and the string is not repeated at the call site.
const defaultMusicBrainzURL = "https://musicbrainz.org"

func NewMusicBrainzService(cfg *config.Config) *MusicBrainzService {
	return &MusicBrainzService{
		cfg:         cfg,
		baseURL:     musicBrainzBaseURL(cfg),
		httpClient:  NewSafeProxyAwareHTTPClient(cfg, 30*time.Second),
		rateLimiter: time.NewTicker(time.Second),
	}
}

func (s *MusicBrainzService) SetCache(cache *CacheService) {
	s.cache = cache
}

// MusicBrainzArtist represents an artist from MusicBrainz.
//
// Country and Type are the fields that disambiguate two artists sharing a
// name. The search endpoint sends both and this struct used to drop them, so
// a candidate list could only ever show a name and a parenthetical.
type MusicBrainzArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
	Type           string `json:"type"`
}

// MusicBrainzRecording represents a recording from MusicBrainz
type MusicBrainzRecording struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Length    int    `json:"length"`
	ReleaseID string `json:"release,omitempty"`
}

// ErrArtistNotFound reports that MusicBrainz has no artist with the given
// ID. It is distinct from a transport or API failure so the UI can tell
// "that is not an artist" apart from "we could not ask".
var ErrArtistNotFound = errors.New("artist not found in MusicBrainz")

// SearchArtist searches MusicBrainz for an artist by name
func (s *MusicBrainzService) SearchArtist(query string) ([]MusicBrainzArtist, error) {

	if s.TestSearch != nil {
		return s.TestSearch(query)
	}
	cacheKey := fmt.Sprintf("artist:%s", query)
	if s.cache != nil {
		var cached []MusicBrainzArtist
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return cached, nil
		}
	}

	// Wait for rate limiter
	<-s.rateLimiter.C

	url := fmt.Sprintf("%s/ws/2/artist?query=artist:%s&fmt=json&limit=5", s.baseURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create musicbrainz request: %w", err)
	}
	userAgent := "netrunner/1.0 (contact@example.com)"
	if s.cfg != nil && s.cfg.MusicBrainzUserAgent != "" {
		userAgent = s.cfg.MusicBrainzUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var result struct {
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			SortName       string `json:"sort-name"`
			Disambiguation string `json:"disambiguation"`
			Country        string `json:"country"`
			Type           string `json:"type"`
		} `json:"artists"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	artists := make([]MusicBrainzArtist, len(result.Artists))
	for i, a := range result.Artists {
		artists[i] = MusicBrainzArtist{
			ID:             a.ID,
			Name:           a.Name,
			SortName:       a.SortName,
			Disambiguation: a.Disambiguation,
			Country:        a.Country,
			Type:           a.Type,
		}
	}

	if s.cache != nil && len(artists) > 0 {
		s.cache.Set("musicbrainz", cacheKey, artists, 24*time.Hour)
	}

	return artists, nil
}

// SearchRecording searches MusicBrainz for a recording (song) by title and artist
func (s *MusicBrainzService) SearchRecording(query string) ([]MusicBrainzRecording, error) {
	cacheKey := fmt.Sprintf("recording:%s", query)
	if s.cache != nil {
		var cached []MusicBrainzRecording
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return cached, nil
		}
	}

	// Wait for rate limiter
	<-s.rateLimiter.C

	url := fmt.Sprintf("%s/ws/2/recording?query=%s&fmt=json&limit=5", s.baseURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create musicbrainz request: %w", err)
	}
	userAgent := "netrunner/1.0 (contact@example.com)"
	if s.cfg != nil && s.cfg.MusicBrainzUserAgent != "" {
		userAgent = s.cfg.MusicBrainzUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var result struct {
		Recordings []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Length   int    `json:"length"`
			Releases []struct {
				ID string `json:"id"`
			} `json:"releases"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
		} `json:"recordings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	recordings := make([]MusicBrainzRecording, len(result.Recordings))
	for i, r := range result.Recordings {
		artistName := ""
		if len(r.Artists) > 0 {
			artistName = r.Artists[0].Name
		}
		releaseID := ""
		if len(r.Releases) > 0 {
			releaseID = r.Releases[0].ID
		}
		recordings[i] = MusicBrainzRecording{
			ID:        r.ID,
			Title:     r.Title,
			Artist:    artistName,
			Length:    r.Length,
			ReleaseID: releaseID,
		}
	}

	if s.cache != nil && len(recordings) > 0 {
		s.cache.Set("musicbrainz", cacheKey, recordings, 24*time.Hour)
	}

	return recordings, nil
}

// GetArtistDiscography gets all release groups for an artist
func (s *MusicBrainzService) GetArtistDiscography(artistID string) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("discography:%s", artistID)
	if s.cache != nil {
		var cached map[string]interface{}
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return cached, nil
		}
	}

	params := url.Values{}
	params.Add("inc", "release-groups")
	params.Add("fmt", "json")

	result, err := s.doRequest(fmt.Sprintf("artist/%s", artistID), params)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set("musicbrainz", cacheKey, result, 12*time.Hour)
	}

	return result, nil
}

func (s *MusicBrainzService) doRequest(endpoint string, params url.Values) (map[string]interface{}, error) {
	// Wait for rate limiter
	<-s.rateLimiter.C

	start := time.Now()

	// Routed through s.baseURL like every other call, NOT a second hardcoded
	// literal. Leaving musicbrainz.org here sent the discography lookup to the
	// REAL service while search and by-id went to the e2e stand-in -- which
	// answered 400 on the stand-in's own MBIDs and failed every scan job.
	baseURL := s.baseURL + "/ws/2/"
	fullURL := fmt.Sprintf("%s%s?%s", baseURL, endpoint, params.Encode())

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		metrics.TrackExternalCall("musicbrainz", start, err)
		return nil, err
	}

	req.Header.Set("User-Agent", s.cfg.MusicBrainzUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		metrics.TrackExternalCall("musicbrainz", start, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		metrics.TrackExternalCall("musicbrainz", start, err)
		return nil, err
	}

	metrics.TrackExternalCall("musicbrainz", start, nil)
	return result, nil
}

func (s *MusicBrainzService) Close() {
	s.rateLimiter.Stop()
}

func (s *MusicBrainzService) HealthCheck() bool {
	// Simple check to see if we can reach MB (could be more thorough)
	return true
}

// GetCoverArt fetches cover art for a release from MusicBrainz
func (s *MusicBrainzService) GetCoverArt(releaseMBID string) (string, error) {
	// First get the release to find the front cover
	release, err := s.GetRelease(releaseMBID)
	if err != nil {
		return "", err
	}

	// Look for front cover in images
	for _, img := range release.Images {
		if img.Front {
			return img.Image, nil
		}
	}

	// Return first image if no front cover found
	if len(release.Images) > 0 {
		return release.Images[0].Image, nil
	}

	return "", fmt.Errorf("no cover art found")
}

// MusicBrainzRelease represents a release from MusicBrainz
type MusicBrainzRelease struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Date         string           `json:"date"`
	Country      string           `json:"country"`
	Genres       []mbGenre        `json:"genres"`
	Media        []mbMedia        `json:"media"`
	Images       []mbImage        `json:"images"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
}

type mbGenre struct {
	Name string `json:"name"`
}

type mbMedia struct {
	Format     string    `json:"format"`
	TrackCount int       `json:"track-count"`
	Tracks     []mbTrack `json:"tracks"`
}

type mbTrack struct {
	Number string `json:"number"`
	Title  string `json:"title"`
}

type mbImage struct {
	Image string `json:"image"`
	Thumb string `json:"thumb"`
	Front bool   `json:"front"`
	Back  bool   `json:"back"`
	Types string `json:"types"`
}

type mbArtistCredit struct {
	Name   string      `json:"name"`
	Artist mbArtistRef `json:"artist"`
}

type mbArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetRelease fetches detailed release information
func (s *MusicBrainzService) GetRelease(releaseMBID string) (*MusicBrainzRelease, error) {
	params := url.Values{}
	params.Set("fmt", "json")
	params.Set("inc", "artist-credit+genres+tracks+images")

	data, err := s.doRequest(fmt.Sprintf("release/%s", releaseMBID), params)
	if err != nil {
		return nil, err
	}

	// Parse into our struct
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var release MusicBrainzRelease
	if err := json.Unmarshal(jsonData, &release); err != nil {
		return nil, err
	}

	return &release, nil
}

// GetReleaseByArtistTitle searches for a release and returns the first result
func (s *MusicBrainzService) GetReleaseByArtistTitle(artist, title string) (*MusicBrainzRelease, error) {
	// Search for release
	query := fmt.Sprintf("artist:%s AND release:%s", artist, title)
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", "1")

	data, err := s.doRequest("release", params)
	if err != nil {
		return nil, err
	}

	releases, ok := data["releases"].([]interface{})
	if !ok || len(releases) == 0 {
		return nil, fmt.Errorf("no release found")
	}

	// Get the MBID of first result
	firstRelease, ok := releases[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid release data")
	}

	mbid, ok := firstRelease["id"].(string)
	if !ok {
		return nil, fmt.Errorf("release has no MBID")
	}

	// Get full release details
	return s.GetRelease(mbid)
}

// GetArtist fetches one artist by MusicBrainz ID.
//
// The candidate picker confirms a choice by ID rather than re-posting the name
// it displayed, so the row that gets stored is the row MusicBrainz holds for
// that ID and not whatever the form claimed. Re-running the search instead
// would work until MusicBrainz re-ranked its results or the cache expired,
// which is exactly the silent-wrong-answer failure this feature exists to end.
func (s *MusicBrainzService) GetArtist(id string) (*MusicBrainzArtist, error) {

	if s.TestGetArtist != nil {
		return s.TestGetArtist(id)
	}
	if id == "" {
		return nil, fmt.Errorf("musicbrainz id is required")
	}

	cacheKey := fmt.Sprintf("artist-by-id:%s", id)
	if s.cache != nil {
		var cached MusicBrainzArtist
		if found, _ := s.cache.Get("musicbrainz", cacheKey, &cached); found {
			return &cached, nil
		}
	}

	<-s.rateLimiter.C

	url := fmt.Sprintf("%s/ws/2/artist/%s?fmt=json", s.baseURL, url.PathEscape(id))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create musicbrainz request: %w", err)
	}
	userAgent := "netrunner/1.0 (contact@example.com)"
	if s.cfg != nil && s.cfg.MusicBrainzUserAgent != "" {
		userAgent = s.cfg.MusicBrainzUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 404 means the ID is not a MusicBrainz artist. That is the caller's
	// mistake, not an outage, and the picker says so rather than offering a
	// retry that would fail the same way.
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrArtistNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz api error: %s", resp.Status)
	}

	var a struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SortName       string `json:"sort-name"`
		Disambiguation string `json:"disambiguation"`
		Country        string `json:"country"`
		Type           string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}

	artist := MusicBrainzArtist{
		ID:             a.ID,
		Name:           a.Name,
		SortName:       a.SortName,
		Disambiguation: a.Disambiguation,
		Country:        a.Country,
		Type:           a.Type,
	}
	if s.cache != nil {
		s.cache.Set("musicbrainz", cacheKey, artist, 24*time.Hour)
	}

	return &artist, nil
}

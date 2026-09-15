package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A Subsonic client browses by following ids: getIndexes/search3 hand out an
// artist id, the client asks getArtist for it, and renders the albums it gets
// back. These tests exercise that chain end to end, because each link was
// broken in a way that looked harmless in isolation: artists came back with no
// name and an unresolvable empty id, albumCount reported tracks instead of
// albums, getArtist answered with an index instead of an artist, and albums
// carried no artistId at all.

type browseIndexResponse struct {
	SubsonicResponse struct {
		Status  string `json:"status"`
		Indexes struct {
			Index []struct {
				Name   string `json:"name"`
				Artist []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					AlbumCount int    `json:"albumCount"`
				} `json:"artist"`
			} `json:"index"`
		} `json:"indexes"`
	} `json:"subsonic-response"`
}

type browseSearchResponse struct {
	SubsonicResponse struct {
		Status        string `json:"status"`
		SearchResult3 struct {
			Artist []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				AlbumCount int    `json:"albumCount"`
			} `json:"artist"`
			Album []struct {
				Name     string `json:"name"`
				Artist   string `json:"artist"`
				ArtistID string `json:"artistId"`
			} `json:"album"`
		} `json:"searchResult3"`
	} `json:"subsonic-response"`
}

type browseArtistXML struct {
	XMLName xml.Name
	Artist  struct {
		ID         string `xml:"id,attr"`
		Name       string `xml:"name,attr"`
		AlbumCount int    `xml:"albumCount,attr"`
		Album      []struct {
			Name     string `xml:"name,attr"`
			ArtistID string `xml:"artistId,attr"`
		} `xml:"album"`
	} `xml:"artist"`
}

func TestSubsonic_Search3_ReturnsNamedArtistsWithResolvableIDs(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	// Album names deliberately contain the query too: search3 matches albums
	// separately from artists, so both legs get exercised by one request.
	createBrowseTestTracks(t, db, "Test Artist", "Test Album One", 2)
	createBrowseTestTracks(t, db, "Test Artist", "Test Album Two", 1)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	req := httptest.NewRequest("GET", "/search3?query=Test&f=json&u=test@example.com&p=testpass123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var parsed browseSearchResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Equal(t, "ok", parsed.SubsonicResponse.Status)

	require.Len(t, parsed.SubsonicResponse.SearchResult3.Artist, 1,
		"exactly one artist matches; a nameless extra entry means the column scan failed")

	artist := parsed.SubsonicResponse.SearchResult3.Artist[0]
	assert.Equal(t, "Test Artist", artist.Name, "the artist's name must survive the query")
	assert.Equal(t, "artist-"+url.PathEscape("Test Artist"), artist.ID,
		"the id must be resolvable, not a bare prefix")
	assert.Equal(t, 2, artist.AlbumCount, "albumCount must count albums, not tracks")

	require.NotEmpty(t, parsed.SubsonicResponse.SearchResult3.Album)
	for _, album := range parsed.SubsonicResponse.SearchResult3.Album {
		assert.Equal(t, "artist-"+url.PathEscape("Test Artist"), album.ArtistID,
			"an album with no artistId leaves a client unable to open its artist")
	}
}

func TestSubsonic_GetIndexes_ArtistsCarryResolvableIDsAndAlbumCounts(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	createBrowseTestTracks(t, db, "Test Artist", "First Album", 3)
	createBrowseTestTracks(t, db, "Test Artist", "Second Album", 1)
	// Lowercase on purpose: the index letter must come from the uppercased first
	// rune, or every lowercase artist lands under "#".
	createBrowseTestTracks(t, db, "kexp", "Live Set", 1)

	app := fiber.New()
	app.Get("/getIndexes", handler.AuthMiddleware, handler.GetIndexes)

	req := httptest.NewRequest("GET", "/getIndexes?f=json&u=test@example.com&p=testpass123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var parsed browseIndexResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Equal(t, "ok", parsed.SubsonicResponse.Status)

	found := map[string]int{}
	letters := map[string]string{}
	for _, idx := range parsed.SubsonicResponse.Indexes.Index {
		for _, artist := range idx.Artist {
			found[artist.Name] = artist.AlbumCount
			letters[artist.Name] = idx.Name
			assert.NotEmpty(t, artist.ID, "artist %q came back with no id", artist.Name)
			assert.NotEqual(t, "artist-", artist.ID)
		}
	}

	assert.Equal(t, 2, found["Test Artist"], "4 tracks across 2 albums must report 2 albums")
	assert.Equal(t, "T", letters["Test Artist"])
	assert.Equal(t, 1, found["kexp"])
	assert.Equal(t, "K", letters["kexp"], "a lowercase artist must not fall into the '#' bucket")
}

func TestSubsonic_GetArtist_ReturnsTopLevelArtistWithAlbums(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	createBrowseTestTracks(t, db, "Test Artist", "First Album", 2)
	createBrowseTestTracks(t, db, "Test Artist", "Second Album", 1)

	app := fiber.New()
	app.Get("/getArtist", handler.AuthMiddleware, handler.GetArtist)

	artistID := "artist-" + url.PathEscape("Test Artist")
	req := httptest.NewRequest("GET", "/getArtist?id="+url.QueryEscape(artistID)+"&u=test@example.com&p=testpass123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	body := string(subsonicGetRespBody(resp))

	var parsed browseArtistXML
	require.NoError(t, xml.Unmarshal([]byte(body), &parsed))
	assert.Equal(t, "Test Artist", parsed.Artist.Name)
	assert.Equal(t, artistID, parsed.Artist.ID, "the id handed out by getIndexes must round-trip")
	assert.Equal(t, 2, parsed.Artist.AlbumCount)
	require.Len(t, parsed.Artist.Album, 2, "getArtist must return the artist's albums")
	if len(parsed.Artist.Album) > 0 {
		assert.Equal(t, artistID, parsed.Artist.Album[0].ArtistID)
	}

	assert.NotContains(t, body, "<indexes",
		"the artist belongs at the top level; an index wrapper is not the getArtist contract")
}

// createBrowseTestTracks adds n tagged tracks to the first library.
func createBrowseTestTracks(t *testing.T, db *gorm.DB, artist, album string, n int) {
	t.Helper()

	var library database.Library
	require.NoError(t, db.First(&library).Error)

	slug := strings.ReplaceAll(strings.ToLower(album), " ", "-")
	for i := 1; i <= n; i++ {
		track := database.Track{
			ID:        uuid.New(),
			Title:     fmt.Sprintf("%s %d", album, i),
			Artist:    artist,
			Album:     album,
			Path:      fmt.Sprintf("/tmp/test-library/%s/%02d.mp3", slug, i),
			LibraryID: library.ID,
			TrackNum:  intPtr(i),
			Format:    "MP3",
			FileSize:  1024000,
		}
		require.NoError(t, db.Create(&track).Error)
	}
}

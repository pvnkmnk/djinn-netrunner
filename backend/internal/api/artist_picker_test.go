package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Add Artist used to resolve an ambiguous name by taking MusicBrainz's first
// result. Measured live: typing "Death" monitored Napalm Death, and the only
// record that a choice had been made was a WARN in the web container's log —
// a file the operator never opens. Everything here pins the replacement: the
// browser is shown the candidates and the confirmed pick is the only one
// stored.
//
// Two facts about the search are load-bearing and easy to get wrong:
//
//   - A failed search is not an empty search. Both used to answer 404 "artist
//     not found in MusicBrainz", which tells the operator their spelling is
//     wrong when the truth is that MusicBrainz was unreachable.
//   - htmx does not swap a 4xx, so every picker state has to arrive at 200 or
//     the operator sees the click do nothing at all.

// pickerTestApp wires the three artist routes against a stub MusicBrainz, so
// the tests never touch the network and can make the search fail on demand.
type pickerTestApp struct {
	app     *fiber.App
	db      *gorm.DB
	user    database.User
	stub    *stubMusicBrainz
	profile database.QualityProfile
}

func newPickerTestApp(t *testing.T) *pickerTestApp {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "picker@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "Default", IsDefault: true}
	require.NoError(t, db.Create(&profile).Error)

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())

	stub := &stubMusicBrainz{artists: []services.MusicBrainzArtist{}, byID: map[string]services.MusicBrainzArtist{}}
	handler := NewArtistsHandler(db,
		services.NewArtistTrackingService(db, services.NewMusicBrainzService(nil)),
		services.NewMusicBrainzService(nil))
	handler.mbService = stub.asService()

	app := fiber.New(fiber.Config{Views: engine})
	inject := func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}
	app.Post("/api/artists/search", inject, handler.Search)
	app.Post("/api/artists", inject, handler.Add)

	return &pickerTestApp{app: app, db: db, user: user, stub: stub, profile: profile}
}

func postForm(t *testing.T, app *fiber.App, target string, form map[string]string, htmx bool) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(urlEncode(form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func postJSONBody(t *testing.T, app *fiber.App, target string, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HX-Request", "true")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func urlEncode(m map[string]string) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw)
}

// The defect itself: an ambiguous name must produce a list, not an artist.
// Asserting the absence of a created row is what makes this fail if the
// silent-first-result behaviour ever comes back.
func TestSearch_AmbiguousNameOffersCandidatesAndCreatesNothing(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-napalm", Name: "Napalm Death", Country: "GB", Type: "Group"},
		{ID: "mbid-death", Name: "Death", Disambiguation: "US death metal band", Country: "US", Type: "Group"},
	}

	resp := postForm(t, h.app, "/api/artists/search", map[string]string{"name": "Death"}, true)
	require.Equal(t, 200, resp.StatusCode, "htmx does not swap a 4xx, so every state must be 200")

	body := bodyOf(t, resp)
	assert.Contains(t, body, "Napalm Death")
	assert.Contains(t, body, "US death metal band",
		"disambiguation is what tells two same-named artists apart")
	assert.Contains(t, body, "Napalm Death", "every candidate must be offered, not only the top one")
	assert.Equal(t, 1, strings.Count(body, "mbid-death"),
		"each candidate needs its own confirm control")

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count, "a search must not create an artist; that is the whole point")
}

// A single unambiguous result is still shown and confirmed. "There was only one"
// is not "I chose that one".
func TestSearch_SingleResultIsStillOfferedNotAutoAccepted(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-only", Name: "Aphex Twin", Country: "GB", Type: "Person"},
	}

	resp := postForm(t, h.app, "/api/artists/search", map[string]string{"name": "Aphex Twin"}, true)
	require.Equal(t, 200, resp.StatusCode)

	body := bodyOf(t, resp)
	assert.Contains(t, body, "mbid-only", "the single candidate must still be offered")
	assert.Contains(t, body, "One artist matched",
		"the copy must say the choice is still the operator's")

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count, "one result is not consent")
}

// Zero results says so and creates nothing.
func TestSearch_NoResultsSaysSoAndCreatesNothing(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = nil

	resp := postForm(t, h.app, "/api/artists/search", map[string]string{"name": "Zzzz Nonexistent"}, true)
	require.Equal(t, 200, resp.StatusCode, "an empty result is still a body htmx must swap")

	body := bodyOf(t, resp)
	assert.Contains(t, body, "No artist matched", "an empty result must say so")
	assert.Contains(t, body, "role=\"status\"")
	assert.NotContains(t, body, "mbid-")

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count)
}

// A failed search must not be reported as an empty one. This is the assertion
// that distinguishes the two: both used to answer 404 "not found".
func TestSearch_FailedSearchIsNotReportedAsNoResults(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.err = assert.AnError

	resp := postForm(t, h.app, "/api/artists/search", map[string]string{"name": "Death"}, true)
	body := bodyOf(t, resp)
	require.Equal(t, 200, resp.StatusCode, "body was: %s", body)
	assert.Contains(t, body, "Could not search MusicBrainz")
	assert.Contains(t, body, "Try again", "a failed search must offer a retry")
	assert.NotContains(t, body, "No artist matched",
		"a failure is not an empty result, and saying so sends the operator to re-check a correct spelling")

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count)
}

// Confirming a pick creates exactly that artist, resolved from MusicBrainz.
func TestAdd_ConfirmedChoiceCreatesOnlyThatArtist(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.byID["mbid-death"] = services.MusicBrainzArtist{
		ID: "mbid-death", Name: "Death", Country: "US", Type: "Group",
	}

	resp := postJSONBody(t, h.app, "/api/artists",
		`{"name":"Death","musicbrainz_id":"mbid-death","quality_profile_id":"`+h.profile.ID.String()+`"}`)
	require.Equal(t, 200, resp.StatusCode)

	var got database.MonitoredArtist
	require.NoError(t, h.db.First(&got).Error)
	assert.Equal(t, "Death", got.Name, "the chosen candidate is the one stored")
	assert.Equal(t, "mbid-death", got.MusicBrainzID)

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "confirming must not also create the other candidates")
}

// The confirmed artist is re-read from MusicBrainz rather than trusted from the
// form, so a client cannot label one ID with another artist's name.
func TestAdd_ConfirmedChoiceIsResolvedFromMusicBrainzNotTheForm(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.byID["mbid-real"] = services.MusicBrainzArtist{ID: "mbid-real", Name: "The Real Name"}

	resp := postJSONBody(t, h.app, "/api/artists",
		`{"name":"A Name The Form Made Up","musicbrainz_id":"mbid-real"}`)
	require.Equal(t, 200, resp.StatusCode)

	var got database.MonitoredArtist
	require.NoError(t, h.db.First(&got).Error)
	assert.Equal(t, "The Real Name", got.Name,
		"the stored name must come from MusicBrainz, not from the request body")
}

// A name-only post still works: docs/DEPLOYMENT.md, the CLI and the MCP server
// all post a bare name, and that contract is documented rather than accidental.
func TestAdd_NameOnlyStillTakesTheTopResult(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-first", Name: "First Result"},
		{ID: "mbid-second", Name: "Second Result"},
	}

	resp := postJSONBody(t, h.app, "/api/artists", `{"name":"anything"}`)
	require.Equal(t, 200, resp.StatusCode)

	var got database.MonitoredArtist
	require.NoError(t, h.db.First(&got).Error)
	assert.Equal(t, "First Result", got.Name,
		"the documented bare-name API contract must keep working")
}

// A search that fails during Add is an upstream failure, not a 404 that says
// the artist does not exist.
func TestAdd_FailedSearchIsNotA404NotFound(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.err = assert.AnError

	resp := postJSONBody(t, h.app, "/api/artists", `{"name":"Death"}`)
	assert.Equal(t, 502, resp.StatusCode,
		"an unreachable MusicBrainz is an upstream failure, not 'no such artist'")

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count)
}

// An empty search really is a 404.
func TestAdd_EmptySearchIsA404(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = nil

	resp := postJSONBody(t, h.app, "/api/artists", `{"name":"Zzzz Nonexistent"}`)
	assert.Equal(t, 404, resp.StatusCode)
}

// Confirming an ID MusicBrainz does not have is the caller's mistake, and says
// so rather than offering a retry that would fail identically.
func TestAdd_UnknownMusicBrainzIDIsA404(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.unknownID = true

	resp := postJSONBody(t, h.app, "/api/artists", `{"name":"x","musicbrainz_id":"nope"}`)
	assert.Equal(t, 404, resp.StatusCode)

	var count int64
	require.NoError(t, h.db.Model(&database.MonitoredArtist{}).Count(&count).Error)
	assert.Zero(t, count)
}

// The picker must carry the profile the operator picked before searching.
func TestSearch_CarriesTheChosenQualityProfileIntoThePicker(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{{ID: "mbid-x", Name: "X"}}

	resp := postForm(t, h.app, "/api/artists/search",
		map[string]string{"name": "X", "quality_profile_id": h.profile.ID.String()}, true)
	require.Equal(t, 200, resp.StatusCode)

	assert.Contains(t, bodyOf(t, resp), h.profile.ID.String(),
		"a confirmed pick must land on the profile the operator chose, not the default")
}

// A name-less search must not reach MusicBrainz, and must not claim the artist
// does not exist.
func TestSearch_EmptyNameRendersWithoutCallingMusicBrainz(t *testing.T) {
	h := newPickerTestApp(t)

	resp := postForm(t, h.app, "/api/artists/search", map[string]string{"name": "   "}, true)
	body := bodyOf(t, resp)

	// A 4xx would not do here: htmx does not swap one, so the region would go
	// silently blank, which is the failure mode this whole slice exists to fix.
	require.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, body, "Type an artist name")
	assert.NotContains(t, body, "Could not search MusicBrainz",
		"nothing was searched, so nothing can have failed")

	assert.Equal(t, 0, h.stub.searchCalls, "an empty query must not reach MusicBrainz")
}

// The form posts form-encoded and the documented API posts JSON. Reading only
// FormValue meant a JSON caller searched for the empty string and was told
// MusicBrainz had failed — found by probing the real endpoint, not by a test.
func TestSearch_AcceptsAJSONBody(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{
		{ID: "b", Name: "Death", Disambiguation: "US death metal band",
			Country: "US", Type: "Group"},
	}

	body := bodyOf(t, postJSON(t, h.app, "/api/artists/search",
		map[string]string{"name": "Death"}, true))

	assert.Contains(t, body, "Death", "a JSON caller must not be searched for the empty string")
	assert.Contains(t, body, "US death metal band")
	assert.Equal(t, 1, h.stub.searchCalls, "the JSON name must reach MusicBrainz verbatim")
}

// The candidate payload must carry the fields the ticket requires; country and
// type are the ones a same-named artist is told apart by.
func TestSearch_CandidatesCarryCountryAndType(t *testing.T) {
	h := newPickerTestApp(t)
	h.stub.artists = []services.MusicBrainzArtist{
		{ID: "a", Name: "Same Name", Disambiguation: "one", Country: "GB", Type: "Group"},
		{ID: "b", Name: "Same Name", Disambiguation: "two", Country: "DE", Type: "Person"},
	}

	body := bodyOf(t, postForm(t, h.app, "/api/artists/search", map[string]string{"name": "Same Name"}, true))
	assert.Contains(t, body, "GB")
	assert.Contains(t, body, "DE")
	assert.Contains(t, body, "Group")
	assert.Contains(t, body, "Person")
}

// stubMusicBrainz replaces the network client so the suite is deterministic and
// can make a search fail on demand.
type stubMusicBrainz struct {
	artists     []services.MusicBrainzArtist
	byID        map[string]services.MusicBrainzArtist
	err         error
	unknownID   bool
	searchCalls int
}

// asService builds a real MusicBrainzService pointed at the stub's behaviour by
// swapping the exported fields the service reads.
func (s *stubMusicBrainz) asService() *services.MusicBrainzService {
	svc := services.NewMusicBrainzService(nil)
	svc.TestSearch = func(string) ([]services.MusicBrainzArtist, error) {
		s.searchCalls++
		if s.err != nil {
			return nil, s.err
		}
		return s.artists, nil
	}
	svc.TestGetArtist = func(id string) (*services.MusicBrainzArtist, error) {
		if s.err != nil {
			return nil, s.err
		}
		if s.unknownID {
			return nil, services.ErrArtistNotFound
		}
		if a, ok := s.byID[id]; ok {
			return &a, nil
		}
		return nil, services.ErrArtistNotFound
	}
	return svc
}

// keep the json import honest if a future test decodes a body
var _ = json.Marshal

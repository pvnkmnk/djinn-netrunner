package api

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The picker is reused, not rebuilt. A destination — where a confirmed pick
// lands — is threaded through search into the candidate row, so the same partial
// and the same four states serve both flows and only the candidates branch
// carries a target at all.
//
// These pin that the threading works in both directions, because the failure
// mode is silent: a row posting to /api/artists when the operator asked to
// re-point would ADD a second monitored artist rather than change the first,
// and nothing in the response would say so.

// newRepointPickerTestApp reuses the picker harness and adds the re-point route,
// so the test exercises the routes as main.go registers them.
func newRepointPickerTestApp(t *testing.T) *pickerTestApp {
	t.Helper()
	p := newPickerTestApp(t)

	handler := NewArtistsHandler(p.db,
		services.NewArtistTrackingService(p.db, services.NewMusicBrainzService(nil)),
		services.NewMusicBrainzService(nil))
	handler.mbService = p.stub.asService()

	inject := func(c *fiber.Ctx) error {
		c.Locals("user", p.user)
		return c.Next()
	}
	p.app.Patch("/api/artists/:id/repoint", inject, handler.Repoint)
	return p
}

// Without a destination the candidate row must still post to Add. This is the
// regression guard for the ordinary flow: threading a destination through must
// not change what happens when there isn't one.
func TestArtistCandidates_WithoutRepointForPostsToAdd(t *testing.T) {
	p := newRepointPickerTestApp(t)
	p.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-a", Name: "Napalm Death", Country: "United Kingdom", Type: "Group"},
	}

	resp := postForm(t, p.app, "/api/artists/search", map[string]string{"name": "death"}, false)
	body := bodyOf(t, resp)

	assert.Contains(t, body, `hx-post="/api/artists"`,
		"with no repoint_for the row must still create an artist")
	assert.NotContains(t, body, "/repoint",
		"no destination was asked for, so nothing should point at re-point")
}

// With a destination the same row must post to the re-point route, carrying the
// monitored artist's ID.
func TestArtistCandidates_WithRepointForPostsToRepoint(t *testing.T) {
	p := newRepointPickerTestApp(t)
	p.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-a", Name: "Napalm Death", Country: "United Kingdom", Type: "Group"},
	}
	p.stub.byID["mbid-a"] = services.MusicBrainzArtist{
		ID: "mbid-a", Name: "Napalm Death", Country: "United Kingdom", Type: "Group",
	}

	resp := postForm(t, p.app, "/api/artists/search", map[string]string{
		"name":        "death",
		"repoint_for": "artist-uuid-123",
	}, false)
	body := bodyOf(t, resp)

	assert.Contains(t, body, `hx-post="/api/artists/artist-uuid-123/repoint"`,
		"the row must post the pick to the re-point route for the chosen artist")
	assert.NotContains(t, body, `hx-post="/api/artists"`,
		"the add route would create a SECOND monitored artist instead of moving this one")
}

// The retry is where the destination is genuinely at risk. searchFailed is the
// one state that offers a retry, so it is the one that can carry the
// destination forward. If it did not, an operator who hit a MusicBrainz outage,
// retried, and succeeded would silently CREATE a second monitored artist
// instead of moving the first.
//
// noMatch deliberately has no retry -- only Cancel -- so there is nothing to
// carry, and asserting otherwise would be asserting a form that does not exist.
func TestArtistSearch_RetryCarriesTheRepointDestination(t *testing.T) {
	p := newRepointPickerTestApp(t)
	p.stub.err = errors.New("musicbrainz is down")

	resp := postForm(t, p.app, "/api/artists/search", map[string]string{
		"name":        "death",
		"repoint_for": "artist-uuid-123",
	}, false)
	body := bodyOf(t, resp)

	assert.Contains(t, body, "Could not search MusicBrainz")
	assert.Contains(t, body, `name="repoint_for"`,
		"the retry must carry repoint_for, or the second attempt adds a row instead of moving one")
	assert.Contains(t, body, `value="artist-uuid-123"`,
		"the retry must carry the destination it was given")
}

// noMatch offers only Cancel. Assert the state is honest about having changed
// nothing, and that it does not offer a retry that would quietly drop the
// destination.
func TestArtistSearch_NoMatchOffersNoRetryAndClaimsNoChange(t *testing.T) {
	p := newRepointPickerTestApp(t)
	p.stub.artists = nil

	resp := postForm(t, p.app, "/api/artists/search", map[string]string{
		"name":        "zzzzz",
		"repoint_for": "artist-uuid-123",
	}, false)
	body := bodyOf(t, resp)

	assert.Contains(t, body, "No artist matched")
	assert.Contains(t, body, "Nothing was added")
	assert.NotContains(t, body, "Try again",
		"noMatch has no retry; if one is added it must carry repoint_for")
	assert.NotContains(t, body, "/repoint",
		"nothing was changed, so nothing should point at re-point")
}

// An empty submit ran no search, so it must say so rather than reporting an
// outage -- and it must claim nothing was added or changed.
func TestArtistSearch_NoNameIsNotAnOutageAndClaimsNoChange(t *testing.T) {
	p := newRepointPickerTestApp(t)

	resp := postForm(t, p.app, "/api/artists/search", map[string]string{
		"repoint_for": "artist-uuid-123",
	}, false)
	body := bodyOf(t, resp)

	assert.Contains(t, body, "Type an artist name to search")
	assert.NotContains(t, body, "Could not search MusicBrainz",
		"no search ran, so this must not read as an outage")
	assert.Contains(t, body, "Nothing was looked up")
	assert.NotContains(t, body, "/repoint",
		"nothing was changed, so nothing should point at re-point")
}

// The card's Re-point control must open the picker with the destination set, or
// the operator picks an entity and it is added rather than applied.
func TestArtistCard_RePointControlOpensThePickerWithADestination(t *testing.T) {
	p := newRepointPickerTestApp(t)
	body := renderCardPartial(t, p, "artist-uuid-123", "Napalm Death")

	assert.Contains(t, body, `hx-post="/api/artists/search"`,
		"Re-point must reuse the existing search endpoint -- there is no new route for opening the picker")
	assert.Contains(t, body, `"repoint_for"`,
		"the control must set repoint_for, or the pick has nowhere to land")
	assert.NotContains(t, body, "js:",
		"htmx compiles js: values with eval, and this app serves script-src 'self'")
}

// renderCardPartial renders the card the same way the handler does, so this
// asserts the markup that ships rather than a copy of it.
func renderCardPartial(t *testing.T, p *pickerTestApp, id, name string) string {
	t.Helper()
	engine := templates.NewPongo2(
		filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())

	var buf strings.Builder
	require.NoError(t, engine.Render(&buf, "partials/artist-card", fiber.Map{"Artist": map[string]any{
		"ID": id, "Name": name, "MusicBrainzID": "mbid-x", "Monitored": true,
		"AcquiredReleases": 1, "TotalReleases": 2, "LastScanDate": nil,
	}}))
	return buf.String()
}

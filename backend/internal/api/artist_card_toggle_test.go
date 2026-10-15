package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// Pressing Pause has to pause (DJI-640, item 2).
//
// The card's Pause control carried no values at all, so htmx sent an empty body,
// Update parsed `monitored` as nil, skipped the write and re-rendered the same
// card: the artist stayed monitored, the label still read "Pause", and
// GET /api/artists still listed it. The defect survived a green Go suite because
// every Go test hands that route a body it wrote itself (handlers_test.go,
// artists_auth_test.go): the route was covered and the control was not.
//
// So this reads the value out of the rendered markup and issues THAT. A control
// declaring nothing sends nothing and the row does not move, which is the
// assertion below rather than a comment about it.
//
// The browser-level version of the same rule, which is where the defect was
// measured, is e2e/tests/artist-monitor-toggle.spec.ts.
func TestArtistCardToggleControls_SendTheStateTheirHandlerApplies(t *testing.T) {
	cases := []struct {
		name      string
		monitored bool
		// sends is what the control must declare: the ABSOLUTE state to move to,
		// not a toggle, because that is what the route applies.
		sends     string
		label     string
		wantAfter bool
		// offers is the label the answer must carry, so the operator can get
		// back: a card that came back still calling itself "Pause" is the
		// symptom this whole test exists for.
		offers string
	}{
		{
			name: "pausing a monitored artist", monitored: true,
			sends: "false", label: "Pause", wantAfter: false, offers: "Resume",
		},
		{
			name: "resuming a stopped artist", monitored: false,
			sends: "true", label: "Resume", wantAfter: true, offers: "Pause",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newArtistCardTestApp(t)

			artist := database.MonitoredArtist{
				MusicBrainzID: "toggle-mbid", Name: "Napalm Death",
				QualityProfileID: p.profile.ID, OwnerUserID: &p.user.ID, Monitored: true,
			}
			require.NoError(t, p.db.Create(&artist).Error)

			// Monitored carries `gorm:"default:true"`, and GORM omits a zero-valued
			// field that declares a default -- so creating this row with
			// Monitored:false would store TRUE. Set the column explicitly and read it
			// back, so the precondition is asserted rather than assumed.
			require.NoError(t, p.db.Model(&database.MonitoredArtist{}).
				Where("id = ?", artist.ID).Update("monitored", tc.monitored).Error)
			var before database.MonitoredArtist
			require.NoError(t, p.db.First(&before, "id = ?", artist.ID).Error)
			require.Equal(t, tc.monitored, before.Monitored,
				"the fixture must start monitored=%v before the control is pressed", tc.monitored)

			region := callHTMX(t, p.app, "GET", "/partials/artists", "", "")
			ctl := toggleControl(t, region)

			assert.True(t, strings.HasPrefix(ctl.label, tc.label),
				"expected the control to be labelled %q, got %q: the label is what the operator reads, so it is also what these expectations key on",
				tc.label, ctl.label)
			assert.Equal(t, tc.sends, ctl.vals["monitored"],
				"the %q control must send the state it wants. With no values htmx sends an EMPTY body, "+
					"the handler reads `monitored` as nil, skips the write, and re-renders the same card",
				tc.label)
			assert.Equal(t, "/api/artists/"+artist.ID.String(), ctl.url,
				"the toggle must name its own row, or the write lands on whatever the route resolves first")

			// The body is derived from those values in cardControls, so this is
			// the request the browser makes.
			body := callHTMX(t, p.app, ctl.verb, ctl.url, ctl.body, ctl.contentType)

			var after database.MonitoredArtist
			require.NoError(t, p.db.First(&after, "id = ?", artist.ID).Error)
			assert.Equal(t, tc.wantAfter, after.Monitored,
				"pressing %q must leave the row at monitored=%v", tc.label, tc.wantAfter)

			assert.Contains(t, body, tc.offers,
				"the card that came back must offer the way back, not repeat the action just taken")
		})
	}
}

// toggleControl returns the card's PATCH control -- the Pause/Resume button.
// Found by verb rather than by label, so a control added beside it does not
// silently become the subject of this test.
func toggleControl(t *testing.T, region string) cardControl {
	t.Helper()

	for _, ctl := range cardControls(t, region) {
		if ctl.verb == "PATCH" {
			return ctl
		}
	}

	require.Fail(t, "the rendered region holds no PATCH control, so there is nothing to press; "+
		"a card without a Pause/Resume button leaves monitoring with no off switch")
	return cardControl{}
}

// newArtistCardTestApp registers the card's own routes on the picker's fixture,
// so each request is issued against the real handler rather than a copy of it.
func newArtistCardTestApp(t *testing.T) *pickerTestApp {
	t.Helper()

	p := newPickerTestApp(t)

	handler := NewArtistsHandler(p.db,
		services.NewArtistTrackingService(p.db, services.NewMusicBrainzService(nil)),
		services.NewMusicBrainzService(nil))
	handler.mbService = p.stub.asService()

	inject := func(c fiber.Ctx) error {
		c.Locals("user", p.user)
		return c.Next()
	}
	p.app.Get("/partials/artists", inject, handler.RenderPartial)
	p.app.Patch("/api/artists/:id", inject, handler.Update)

	return p
}

// cardControl is one control's declaration, read out of the rendered markup.
//
// vals and body carry what the control SENDS: htmx merges a control's hx-vals
// into the request body, so the body here is derived from the markup rather than
// written by the test. A test that supplies its own body can only ever check the
// handler, never the control -- which is how the card's Pause button shipped
// sending nothing at all.
type cardControl struct {
	verb        string
	url         string
	target      string
	swap        string
	label       string
	vals        map[string]string
	body        string
	contentType string
}

// formBody encodes a control's values the way htmx does for a form-encoded
// request. url.Values sorts by key, so the encoded body is stable.
func formBody(vals map[string]string) string {
	if len(vals) == 0 {
		return ""
	}
	form := url.Values{}
	for k, v := range vals {
		form.Set(k, v)
	}
	return form.Encode()
}

// cardControls reads every control's declaration out of the rendered region,
// through an HTML parser rather than a regex: the card's Re-point button spans
// several lines and carries a single-quoted hx-vals, and matching substrings over
// that is how a guard ends up asserting a string no browser ever acts on.
func cardControls(t *testing.T, region string) []cardControl {
	t.Helper()

	doc, err := html.Parse(strings.NewReader(region))
	require.NoError(t, err, "the rendered region must parse")

	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}

	var controls []cardControl
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "button" {
			ctl := cardControl{
				target: attr(n, "hx-target"),
				swap:   attr(n, "hx-swap"),
				label:  attr(n, "aria-label"),
			}
			for _, verb := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
				url := attr(n, "hx-"+strings.ToLower(verb))
				if url == "" {
					continue
				}
				ctl.verb, ctl.url = verb, url
				// htmx merges hx-vals into the request body and encodes
				// mutating requests as a form. A control that declares no
				// values still sends that content type with an EMPTY body --
				// which is exactly the request this test has to issue to see
				// the defect.
				if verb != "GET" {
					ctl.contentType = "application/x-www-form-urlencoded"
				}
				if vals := attr(n, "hx-vals"); vals != "" {
					// A plain JSON object, never the values-expression form
					// htmx compiles with eval: this app serves script-src
					// 'self', so an expression throws at the click and issues
					// NO request at all (csp_template_test.go holds that ban
					// repo-wide). If this parse fails, the browser's would too.
					var parsed map[string]string
					require.NoErrorf(t, json.Unmarshal([]byte(vals), &parsed),
						"hx-vals on the %q control is not a plain JSON object: %.80s", ctl.label, vals)
					ctl.vals = parsed
					ctl.body = formBody(parsed)
				}
				break
			}
			if ctl.verb != "" {
				if ctl.label == "" {
					ctl.label = "an unlabelled control"
				}
				controls = append(controls, ctl)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return controls
}

// callHTMX issues one control's request the way the browser does and returns the
// body. A 4xx is a failure here for the same reason it matters in the browser:
// htmx does not swap one, so the control would do nothing at all.
//
// The content type is the control's own: htmx sends a form-encoded body for the
// decoders this app declares with `form:` tags, and sending JSON instead would
// test a transport the control never uses.
func callHTMX(t *testing.T, app *fiber.App, verb, url, body, contentType string) string {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(verb, url, reader)
	req.Header.Set("HX-Request", "true")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Less(t, resp.StatusCode, 400,
		"%s %s answered %d; htmx does not swap a 4xx, so the control would appear to do nothing", verb, url, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw)
}

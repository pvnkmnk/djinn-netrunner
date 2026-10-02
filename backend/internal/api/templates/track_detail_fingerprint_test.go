package templates

import (
	"strings"
	"testing"

	"github.com/flosch/pongo2/v6"
	"github.com/stretchr/testify/require"
)

// DJI-545, AC4 on the browser surface. The AcoustID Fingerprint row was
// conditional on the value being non-empty, so an unfingerprinted track and a
// fingerprinted one rendered the *same page* minus a line - and an omission
// reads as "nothing to report". That mattered here more than anywhere: for the
// project's whole history the image had no fpcalc, so every fingerprint was
// missing globally as a deployment fault, and the one surface that could have
// said so was silent.
func TestTrackDetailStatesWhenATrackWasNotFingerprinted(t *testing.T) {
	engine := testEngine(t)

	render := func(fingerprint string) string {
		return renderTemplate(t, engine, "partials/track-detail", pongo2.Context{
			"track": map[string]any{
				"ID":          "11111111-1111-1111-1111-111111111111",
				"Title":       "Faceless",
				"Artist":      "Overcast",
				"Album":       "Twin Terror",
				"FileHash":    "15411cc7e6d43f2d5edbacf8b9fa4788",
				"Fingerprint": fingerprint,
			},
		})
	}

	fingerprinted := render("AQADtEmqSImp4d8yAQADtEmqSImp4d8y")
	require.Contains(t, fingerprinted, "AcoustID Fingerprint")
	require.Contains(t, fingerprinted, "AQADtEmqSImp4d8y",
		"a real fingerprint must be shown, unchanged")

	unfingerprinted := render("")
	require.Contains(t, unfingerprinted, "AcoustID Fingerprint",
		"the row must still be there: its absence reads as nothing to report")
	require.Contains(t, unfingerprinted, "not fingerprinted",
		"an absent fingerprint must be stated in words, not left to inference")

	// The honest state must not borrow the monospace value styling - it is a
	// fact about the track, not a hash, and this is the class the stylesheet
	// already defines for it.
	require.True(t, strings.Contains(unfingerprinted, `class="detail-value small"`),
		"the absent state should use the existing secondary-value styling")
}

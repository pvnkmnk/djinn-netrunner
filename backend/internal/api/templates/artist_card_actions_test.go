package templates

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// Every button on the artist card is an action the operator can take, so every
// button must render a visible label. One shipped that did not: the Sync
// button's opening tag was never closed, so an HTML parser auto-closed it at
// the Re-point button that followed. Sync rendered as an empty box and its
// "Sync" text became an orphan sibling of the buttons. The full e2e suite
// stayed green, because a browser spec asserts that a button exists and what
// it posts -- never what is printed inside it.
//
// The card's tags are BALANCED in the broken version: four open, four close.
// So a count of "<button" against "</button>" passes on the defect that
// actually shipped and reads as a passing guard. This parses the rendered card
// the way a browser does and inspects the resulting tree, which is the only
// place the damage is visible.
func TestArtistCard_EveryButtonRendersALabel(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	engine := NewPongo2(templatesDir, ".html")

	body := renderPartial(t, engine, "partials/artist-card.html", map[string]any{
		"Artist": map[string]any{
			"ID": "artist-1", "Name": "Napalm Death", "MusicBrainzID": "mbid-1",
			"Monitored": true, "AcquiredReleases": 12, "TotalReleases": 48,
			"LastScanLabel": "3 Oct 2026",
		},
	})

	doc, err := html.Parse(strings.NewReader(body))
	require.NoError(t, err, "the rendered card must parse")

	type action struct {
		label    string
		verb     string
		nestedIn bool
	}
	var found []action

	// attr returns the first matching attribute value, or "".
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}

	// walk collects every button, flagging any nested inside another one.
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inButton bool) {
		if n.Type == html.ElementNode && n.Data == "button" {
			var label strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					label.WriteString(c.Data)
				}
			}
			verb := attr(n, "hx-post")
			if verb == "" {
				verb = attr(n, "hx-patch")
			}
			if verb == "" {
				verb = attr(n, "hx-delete")
			}
			found = append(found, action{
				label:    strings.TrimSpace(label.String()),
				verb:     verb,
				nestedIn: inButton,
			})
			inButton = true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inButton)
		}
	}
	walk(doc, false)

	require.NotEmpty(t, found, "the card must render action buttons")

	for _, b := range found {
		assert.False(t, b.nestedIn,
			"a button nested inside another button (hx-%s) is auto-closed by the parser, "+
				"which empties the outer one", b.verb)
		assert.NotEmpty(t, b.label,
			"button with hx-%s renders no visible label; an aria-label alone leaves an empty box", b.verb)
	}

	// The four actions the card is supposed to offer, each with its own verb.
	verbs := make([]string, 0, len(found))
	for _, b := range found {
		verbs = append(verbs, b.verb)
	}
	assert.Len(t, verbs, 4,
		"expected exactly four actions (sync, re-point, pause/resume, remove), got %v", verbs)
	assert.NotEqual(t, "", verbs[0],
		"the first action on the card must post to the sync endpoint")
}

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
			"ID": "a1", "Name": "Napalm Death", "MusicBrainzID": "mbid-1",
			"Monitored": true, "AcquiredReleases": 12, "TotalReleases": 48,
			"LastScanLabel": "3 Oct 2026",
		},
	})

	doc, err := html.Parse(strings.NewReader(body))
	require.NoError(t, err, "the rendered card must parse")

	type action struct {
		label    string
		verb     string
		target   string
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
			verb := ""
			for _, m := range []string{"GET", "POST", "PATCH", "DELETE"} {
				if v := attr(n, "hx-"+strings.ToLower(m)); v != "" {
					verb = m + " " + v
					break
				}
			}
			found = append(found, action{
				label:    strings.TrimSpace(label.String()),
				verb:     verb,
				target:   attr(n, "hx-target"),
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
			"a button nested inside another button (%s) is auto-closed by the parser, "+
				"which empties the outer one", b.verb)
		assert.NotEmpty(t, b.label,
			"the %s button renders no visible label; an aria-label alone leaves an empty box", b.verb)
	}

	// Each label must be bound to the endpoint it names. Asserting only that
	// the first button posts *somewhere* lets a control labelled "Sync" that
	// queues a different action pass: the operator reads "Sync" and gets
	// something else, with nothing in the tree to catch the mismatch.
	want := []struct {
		label  string
		verb   string
		target string
	}{
		{"Sync", "POST /api/artists/a1/sync", "#notice"},
		{"Re-point", "GET /partials/artist-form", "#modal-container"},
		{"Pause", "PATCH /api/artists/a1", "#artist-a1"},
		{"Remove", "DELETE /api/artists/a1", "#artist-a1"},
	}

	got := make([]string, 0, len(found))
	for _, b := range found {
		got = append(got, b.label)
	}
	require.Len(t, found, len(want),
		"expected exactly four actions (sync, re-point, pause/resume, remove), got %v", got)

	for i, w := range want {
		assert.Equal(t, w.label, found[i].label, "action %d label", i)
		assert.Equal(t, w.verb, found[i].verb, "the %q button must reach %s", w.label, w.verb)
		assert.Equal(t, w.target, found[i].target, "the %q button must swap into %s", w.label, w.target)
	}
}

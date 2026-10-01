package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two places where the page told the user to do something it offered no way to
// do, and both are the same defect wearing different clothes: a reply, or an
// instruction, that goes nowhere the user is looking.
//
// The dashboard's Attach button carried hx-post with no hx-target, so htmx
// swapped the reply into the button itself. Clicking it replaced the label
// "Attach" with the reply text, which also became the button's accessible
// name, while the console region still read "Select a job to attach" and
// #notice stayed empty. The click read as doing nothing while quietly
// dismantling the control.
//
// The browse empty state read "Run a scan to populate the library" and offered
// no scan; the Scan button lives on the library card, one navigation back.
//
// These read the templates rather than the served HTML, deliberately: a control
// inside a pongo2 branch is only sometimes rendered, and a guard that has to
// guess when would miss it.

// attachButtonRE matches the dashboard's Attach control, in full, so its
// attributes can be asserted as a set rather than as loose substrings.
var attachButtonRE = regexp.MustCompile(`(?s)<button\b[^>]*\bid="btn-attach"[^>]*>.*?</button>`)

// attributeRE pulls one attribute's value out of a control's markup.
func attributeRE(name string) *regexp.Regexp {
	return regexp.MustCompile(name + `="([^"]*)"`)
}

// regionRE matches the console region the Attach reply lands in.
var regionRE = regexp.MustCompile(`(?s)<div[^>]*id="console-socket".*?>`)

// TestAttachReplyLandsInTheConsoleRegionNotTheButton is the guard for the
// first defect. An htmx control with no hx-target defaults to targeting itself,
// which is what destroyed the label: the reply became the button's contents
// and its accessible name. The reply has to go to the region that shows the
// console, and that region has to exist.
func TestAttachReplyLandsInTheConsoleRegionNotTheButton(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	raw, err := os.ReadFile(filepath.Join(templatesDir, "index.html"))
	require.NoError(t, err)
	page := string(raw)

	button := attachButtonRE.FindString(page)
	require.NotEmpty(t, button, "the Attach button is gone from index.html; "+
		"if it was removed deliberately, the console needs a different way to "+
		"report what it is doing")

	match := attributeRE("hx-target").FindStringSubmatch(button)
	require.Len(t, match, 2, "the Attach button has no hx-target, so htmx will "+
		"swap the reply into the button and destroy its label (DJI-532)")

	assert.Equal(t, "#console-socket", match[1],
		"the Attach reply belongs in the console region; anything else puts it "+
			"somewhere the user is not looking")

	assert.Contains(t, page, `id="console-socket"`,
		"the Attach button targets #console-socket but no element carries that "+
			"id, so the reply would be appended to nothing and the region would "+
			"still read 'Select a job to attach'")

	// Shown is not the same as announced. The region holds either the
	// placeholder or the reply, so it is exactly the kind of node aria-live
	// exists for, and every other piece of changing feedback in this app is
	// already live: #console-logs, #status-announcer, #notice.
	socket := regionRE.FindString(page)
	require.NotEmpty(t, socket, "could not find the #console-socket element")
	assert.Contains(t, socket, `aria-live="polite"`,
		"the attach reply is swapped into #console-socket but that region is "+
			"not a live region, so a screen-reader user never hears it (DJI-532)")
}

// hx-disabled-elt is deliberately absent from the Attach button.
//
// It reads like the pending state, and on this htmx build it is not one. Live on
// 2026-10-01: the attribute never applies `disabled`, and htmx's own
// `htmx-request` class - `opacity: 0.7; cursor: wait !important` - is added to
// the issuing element and, because the reply now swaps into a different
// target, never removed. The control is left permanently dimmed and claiming to
// be busy. The watchlist SYNC button, which has carried the attribute all
// along, behaves identically, so this is the app-wide indicator lifecycle
// rather than anything about Attach.
//
// A test here would only assert that an inert attribute is still present. The
// lifecycle is filed as DJI-563; if it lands and hx-disabled-elt starts working,
// this is the place to write down what "pending" has to mean.

// browseEmptyStateRE isolates the two arms of the empty state: the no-results
// search on the left, the never-scanned library on the right.
//
// It is anchored to the <div class="empty-state"> that opens the block, and
// that anchor is load-bearing rather than decoration. The partial also has an
// unrelated one-line `{% if search %}` in its track-count header, and a looser
// pattern runs from that one all the way down to the empty state's {% else %},
// quietly testing an arm that spans most of the template.
var browseEmptyStateRE = regexp.MustCompile(
	`(?s)<div class="empty-state">\s*\{%\s*if search\s*%\}(.*?)\{%\s*else\s*%\}(.*?)\{%\s*endif\s*%\}`)

// browseEmptyState returns the search arm and the unscanned arm of the browse
// partial's empty state. It fails if the shape changes, rather than quietly
// testing a different branch than the one the defect was in.
func browseEmptyState(t *testing.T) (searchArm, unscannedArm string) {
	t.Helper()

	templatesDir, _ := webAssetPaths(t)
	raw, err := os.ReadFile(filepath.Join(templatesDir, "partials", "library-browse.html"))
	require.NoError(t, err)

	matches := browseEmptyStateRE.FindAllStringSubmatch(string(raw), -1)
	require.Len(t, matches, 1,
		"expected exactly one .empty-state search/unscanned pair in "+
			"library-browse.html; the arms have moved", len(matches))

	return matches[0][1], matches[0][2]
}

// scanPostRE matches a control that triggers a library scan.
var scanPostRE = regexp.MustCompile(`hx-post="(/api/libraries/\{\{ library\.ID \}\}/scan)"`)

// TestUnscannedBrowseOffersTheScanItsCopyNames is the guard for the second
// defect. The copy tells the user to run a scan, so the scan has to be right
// there - and it has to report back somewhere, which is what hx-target is for.
func TestUnscannedBrowseOffersTheScanItsCopyNames(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	raw, err := os.ReadFile(filepath.Join(templatesDir, "partials", "library-browse.html"))
	require.NoError(t, err)
	page := string(raw)

	_, unscanned := browseEmptyState(t)

	assert.Contains(t, unscanned, "Run a scan to populate the library",
		"the unscanned branch no longer mentions scanning")
	require.Len(t, scanPostRE.FindAllStringSubmatch(unscanned, -1), 1,
		"the empty state tells the user to run a scan but offers no way to run "+
			"one - the Scan button is on the library card, one navigation back "+
			"(DJI-532)")

	target := attributeRE("hx-target").FindStringSubmatch(unscanned)
	require.Len(t, target, 2, "the scan button has no hx-target, so the "+
		"confirmation will replace the button instead of appearing beside it")

	id := strings.TrimPrefix(target[1], "#")
	assert.NotEmpty(t, id)
	assert.Contains(t, page, `id="`+id+`"`,
		"the scan button targets #%s but no element carries that id, so the "+
			"scan confirmation will be appended to nothing", id)
}

// TestASearchWithNoResultsDoesNotOfferAScan keeps the two arms honest. A
// search that matched nothing is a different problem from a library that has
// never been scanned: the library may be full of tracks that do not match the
// query, and offering Scan there invites a pointless full scan.
func TestASearchWithNoResultsDoesNotOfferAScan(t *testing.T) {
	searchArm, _ := browseEmptyState(t)

	assert.NotContains(t, searchArm, "/scan",
		"the no-results search arm offers a library scan; a search that matched "+
			"nothing is not an unscanned library (DJI-532)")
}

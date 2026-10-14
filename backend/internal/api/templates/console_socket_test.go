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

// DJI-562. The dashboard's live console was wired to a route that does not
// exist, through an extension this build does not contain: index.html carried
// hx-ext="ws" with ws-connect="/ws/jobs", while the server registers
// /ws/jobs/:job_id. "ws-connect" appears zero times in the htmx this app ships
// (htmx.min.js is 1.9.10 core), so nothing connected, and nothing reported that
// nothing connected - an inert socket and a 404 look identical from a console
// that is empty either way.
//
// The shape that is true now: the page cannot name a job, so it does not try.
// It asks the server for the caller's streamable jobs (/partials/console-jobs),
// posts the pick to /console/attach, and the reply - not the page - carries the
// socket element, with the URL the handler built from the job it resolved and
// ownership-checked. These read the templates rather than the served HTML,
// deliberately: these relationships are between files.
//
// Every scan here runs over stripTemplateNoise, because these tests' own
// explanation of the defect is written in the templates as comments, and a
// comment cannot open a socket.

// templateFile reads one template relative to ops/web/templates, comments
// removed.
func templateFile(t *testing.T, name string) string {
	t.Helper()
	templatesDir, _ := webAssetPaths(t)
	raw, err := os.ReadFile(filepath.Join(templatesDir, filepath.FromSlash(name)))
	require.NoError(t, err)
	return stripTemplateNoise(string(raw))
}

// joblessSocketRE matches the console socket path with nothing naming a job.
var joblessSocketRE = regexp.MustCompile(`/ws/jobs["'?\s]`)

// TestConsolePageDoesNotOpenItsOwnSocket keeps the page out of the socket
// business. It has no job to name, and the two dead attributes it used to carry
// are the reason this file exists.
func TestConsolePageDoesNotOpenItsOwnSocket(t *testing.T) {
	page := templateFile(t, "index.html")

	assert.NotContains(t, page, "ws-connect",
		"ws-connect belongs to htmx's ws extension, which this app does not ship: "+
			"an element carrying it is inert, not wired")
	assert.NotContains(t, page, "hx-ws",
		"the socket URL carries a job id, and the dashboard has no job to name - "+
			"the attach reply is the only place allowed to open the stream")

	// The two requests the page does make, and the selection the second one
	// needs to have anything to resolve.
	assert.Contains(t, page, `hx-get="/partials/console-jobs"`,
		"the picker is how the operator names a job")
	assert.Contains(t, page, `hx-post="/console/attach"`)
	assert.Contains(t, page, `hx-include="#console-job-picker"`,
		"an attach that does not send the selection is a button that can only refuse")
}

// TestTheAttachReplyCarriesTheServerBuiltSocketURL is the acceptance criterion
// for the item: the reply names a route that exists, with the job in it.
func TestTheAttachReplyCarriesTheServerBuiltSocketURL(t *testing.T) {
	reply := templateFile(t, "partials/console-attach.html")

	match := attributeRE("hx-ws").FindStringSubmatch(reply)
	require.Len(t, match, 2,
		"the attach reply is what opens the console socket; without hx-ws the "+
			"region says \"Attached\" and streams nothing (DJI-562)")
	assert.Equal(t, "connect:{{ WSURL }}", match[1],
		"the URL has to be the handler's: it names the job that was resolved and "+
			"ownership-checked, which a template cannot know")
}

// TestTheAttachReplyClearsThePaneItIsTakingOver: attaching while attached is
// normal (watch one job, then another), and two jobs' lines interleaved in one
// pane is not a log of either.
func TestTheAttachReplyClearsThePaneItIsTakingOver(t *testing.T) {
	reply := templateFile(t, "partials/console-attach.html")
	page := templateFile(t, "index.html")

	var oobElementRE = regexp.MustCompile(`(?s)<[^>]*hx-swap-oob[^>]*>`)
	oob := oobElementRE.FindString(reply)
	require.NotEmpty(t, oob,
		"the attach reply has to clear the log pane it is taking over")

	idMatch := regexp.MustCompile(`\sid="([^"]+)"`).FindStringSubmatch(oob)
	require.Len(t, idMatch, 2, "the cleared pane has to be named: %s", oob)

	assert.Contains(t, page, `id="`+idMatch[1]+`"`,
		"the attach reply clears #%s, and the page is the only place that declares it", idMatch[1])
}

// TestNoTemplateHardcodesAJoblessSocketRoute is the guard for the defect
// itself: /ws/jobs on its own is not a route, and a template is not where a job
// id can come from.
func TestNoTemplateHardcodesAJoblessSocketRoute(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	var offenders []string
	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if joblessSocketRE.MatchString(stripTemplateNoise(string(body))) {
			rel, relErr := filepath.Rel(templatesDir, path)
			if relErr != nil {
				return relErr
			}
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"these templates name a console socket without naming a job; the URL is "+
			"built by api.ConsoleHandler.Attach from the job it resolved, so a "+
			"template that writes one itself is wiring a route that does not exist: %v",
		offenders)
}

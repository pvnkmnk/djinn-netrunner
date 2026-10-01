package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/flosch/pongo2/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Six pages rendered their heading twice: once statically in the page template
// and once in the htmx partial that template loads over the top of it. The
// duplication only appears after the swap, so a fetch-and-parse of the page
// templates shows a single heading on each and nothing complains. That is why
// this test renders both halves and counts, rather than reading one file.
//
// The two halves are counted together because that is what the browser ends up
// holding: the page document with the partial swapped into its region. Exactly
// one page-heading element may come out of that pair, whichever template it
// came from. Jobs puts its heading in the page template because its partial has
// none and its action button needs somewhere to live; the six others put it in
// the partial, which is where their action buttons already are.
//
// pageHeading matches the class on the heading element itself, so a class
// mentioned in a comment or in the stylesheet cannot satisfy it.
var pageHeading = regexp.MustCompile(`<h2[^>]*class="page-heading"`)

// pagePartialPairs is every page that loads a partial over its own region. A
// page added without a row here gets no protection, which is what
// TestEveryPageThatLoadsAPartialIsListed exists to notice.
var pagePartialPairs = []struct{ page, partial string }{
	{"pages/artists.html", "partials/artists.html"},
	{"pages/libraries.html", "partials/libraries.html"},
	{"pages/playlists.html", "partials/playlists.html"},
	{"pages/profiles.html", "partials/profiles.html"},
	{"pages/schedules.html", "partials/schedules.html"},
	{"pages/watchlists.html", "partials/watchlists.html"},
	{"pages/jobs.html", "partials/jobs.html"},
}

func testEngine(t *testing.T) *Pongo2Engine {
	t.Helper()
	templatesDir, _ := webAssetPaths(t)
	return NewPongo2(templatesDir, ".html")
}

// renderTemplate renders one template the way the server does, with the minimum
// context the layout and the partial need.
func renderTemplate(t *testing.T, engine *Pongo2Engine, name string, ctx pongo2.Context) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, engine.Render(&buf, name, ctx),
		"rendering %s failed; the test cannot say anything about a page it could not build", name)
	return buf.String()
}

// pageContext is the context every page template needs. Page drives the nav
// highlight, so it is set to the page's own name, and authUserID is set so a
// page renders its signed-in form rather than the sign-in screen.
func pageContext(page string) pongo2.Context {
	return pongo2.Context{
		"Page":             page,
		"authUserID":       "1",
		"IsAdmin":          false,
		"Version":          "test",
		"CSRFToken":        "token",
		"CurrentUserEmail": "",
	}
}

func pageName(page string) string {
	return strings.TrimSuffix(strings.TrimPrefix(page, "pages/"), ".html")
}

func TestNoPageRendersItsHeadingTwice(t *testing.T) {
	engine := testEngine(t)

	for _, pair := range pagePartialPairs {
		t.Run(pair.page, func(t *testing.T) {
			page := renderTemplate(t, engine, pair.page, pageContext(pageName(pair.page)))
			partial := renderTemplate(t, engine, pair.partial, pongo2.Context{})

			total := len(pageHeading.FindAllString(page, -1)) + len(pageHeading.FindAllString(partial, -1))
			assert.Equal(t, 1, total,
				"%s and %s between them render %d page headings; exactly one is allowed. "+
					"The page template and the partial it loads must not both name the page",
				pair.page, pair.partial, total)
		})
	}
}

// A guard is only as good as the table behind it. The half that can duplicate
// a heading is the partial that renders one, so every partial containing an
// <h2> must be paired with its page - otherwise nothing checks whether that
// page also renders the same heading.
//
// Keyed on the partial rather than on the page on purpose: the admin page loads
// three partials and none of them render a heading, so there is nothing for it
// to duplicate and demanding a row for it would be the guard being blunt
// rather than the table being incomplete.
// headingPartialExemptions are heading-bearing partials that are not the
// heading half of any page, with the reason each one is safe.
var headingPartialExemptions = map[string]string{
	"partials/library-browse.html": "detail view requested by partials/libraries.html " +
		"(hx-get=\"/partials/libraries/{id}/browse\", hx-target=\"#libraries-region\"); " +
		"it replaces the region, so 'Browse: <name>' takes the place of the " +
		"'Libraries' heading rather than stacking on it",
}

func TestEveryHeadingPartialIsCovered(t *testing.T) {
	covered := map[string]bool{}
	for _, pair := range pagePartialPairs {
		covered[pair.partial] = true
	}

	templatesDir, _ := webAssetPaths(t)
	entries, err := os.ReadDir(filepath.Join(templatesDir, "partials"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "no partial templates found; the path is probably wrong")

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(templatesDir, "partials", entry.Name()))
		require.NoError(t, err)
		rel := "partials/" + entry.Name()
		rendersHeading := strings.Contains(string(raw), "<h2")

		if reason, exempt := headingPartialExemptions[rel]; exempt {
			assert.True(t, rendersHeading,
				"%s is in headingPartialExemptions but no longer renders a heading; "+
					"remove the exemption (%s)", rel, reason)
			continue
		}
		if !rendersHeading {
			continue
		}
		assert.True(t, covered[rel],
			"%s renders a heading but is not the partial half of a pagePartialPairs row "+
				"and not in headingPartialExemptions, so TestNoPageRendersItsHeadingTwice "+
				"says nothing about it", rel)
	}
}

// The index template serves three states - signed in, signing in, and creating
// an account - and named all of them "Dashboard". An anonymous visitor was
// looking at a sign-in screen with a Dashboard tab above it.
func TestAnonymousIndexIsNotTitledDashboard(t *testing.T) {
	engine := testEngine(t)

	// authUserID must be present and empty. pongo2 evaluates an absent key as
	// not equal to "", so a context that simply omits it renders the signed-in
	// dashboard and this test would pass for the wrong reason.
	anonymous := renderTemplate(t, engine, "index.html", pongo2.Context{"authUserID": ""})
	assert.Contains(t, anonymous, "<title>Sign In - NETRUNNER</title>",
		"the sign-in screen must say what it is")
	assert.NotContains(t, anonymous, "<title>Dashboard - NETRUNNER</title>",
		"the anonymous screen must not claim to be the Dashboard")

	signedIn := renderTemplate(t, engine, "index.html", pageContext("dashboard"))
	assert.Contains(t, signedIn, "<title>Dashboard - NETRUNNER</title>",
		"the signed-in dashboard still calls itself the Dashboard")
}

// A tab that disagrees with the heading is the other half of "honest titles".
// The heading is what the person is looking at, so the tab names that.
func TestEveryPageTitleNamesItsOwnHeading(t *testing.T) {
	engine := testEngine(t)

	for _, pair := range pagePartialPairs {
		t.Run(pair.page, func(t *testing.T) {
			page := renderTemplate(t, engine, pair.page, pageContext(pageName(pair.page)))
			partial := renderTemplate(t, engine, pair.partial, pongo2.Context{})

			// The heading is whichever half carries it.
			heading := headingText(page + partial)
			require.NotEmpty(t, heading,
				"no page heading in %s + %s", pair.page, pair.partial)

			title := between(page, "<title>", "</title>")
			assert.True(t, strings.HasPrefix(title, heading),
				"the tab says %q but the page's heading is %q; the tab should name the heading",
				title, heading)
		})
	}
}

// headingText returns the text of the first page heading in a rendered
// document, dropping any conditional the heading carried.
func headingText(html string) string {
	loc := pageHeading.FindStringIndex(html)
	if loc == nil {
		return ""
	}
	rest := html[loc[1]:]

	open := strings.Index(rest, ">")
	if open < 0 {
		return ""
	}
	// Search for the closing bracket after the tag ends. From the start of the
	// match the only "<" is the opening one.
	inner := rest[open+1:]
	closing := strings.Index(inner, "<")
	if closing < 0 {
		return ""
	}
	text := inner[:closing]
	// Jobs' heading carries "{% if IsAdmin %}All Users{% endif %}". Keep the
	// heading itself and drop the conditional body.
	if i := strings.Index(text, "{%"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// between returns the text between two markers, or "" when either is absent.
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

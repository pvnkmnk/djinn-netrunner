package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file guards one invariant: every value a template renders is escaped
// exactly once.
//
// Pongo2 autoescapes by default - `var autoescape = true` at package level, and
// `newExecutionContext` copies it onto every execution context - and this engine
// never calls SetAutoescape(false). The autoescape path *is* the escape filter:
// variable.go applies `filters["escape"]` when the value is a string and no
// `safe` filter was applied. So an explicit `{{ x | escape }}` ran the filter a
// second time and the operator read the entity.
//
// That is what shipped: a monitored artist named `Converge & Chelsea Wolfe`
// painted as `Converge &amp; Chelsea Wolfe` on the artists card, `'` became
// `&#39;`, and 130 explicit filters across 21 templates did it in text and in
// aria-label, data-* and value attributes alike. The filter is byte-for-byte
// redundant with what the engine already does, so the fix was to delete it.
//
// Deleting it is only correct while the engine keeps escaping. Hence three
// guards rather than one: the escaping is asserted behaviourally against the
// real partials, the filter cannot come back, and the one call that would
// disarm the engine cannot appear in a non-test file.

// escapedOnce is the exact output of pongo2's escape filter for the five
// characters it touches (filters_builtin.go: &, >, <, " and ' in that order).
const escapingProbe = `Tom & Jerry <script>alert("x")</script> it's`

var escapingProbeEscapedOnce = []string{
	"Tom &amp; Jerry",
	"&lt;script&gt;",
	"&quot;x&quot;",
	"it&#39;s",
}

// escapingProbeEscapedTwice is the same characters after a second pass. This is
// the defect itself, and every assertion below is the difference between the two
// lists.
var escapingProbeEscapedTwice = []string{
	"Tom &amp;amp; Jerry",
	"&amp;lt;script&amp;gt;",
	"&amp;quot;x&amp;quot;",
	"it&amp;#39;s",
}

// TestPartials_RenderEveryValueEscapedExactlyOnce renders the real artists
// partial, the surface the defect was reported on, through the real engine.
//
// It uses the template file rather than an inline string on purpose: the
// regression this catches is a filter someone adds back to a template, and a
// test rendering its own copy of the markup would not see it.
func TestPartials_RenderEveryValueEscapedExactlyOnce(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	body := renderTemplateFile(t, templatesDir, "partials/artists.html", map[string]any{
		"artists": []map[string]any{{
			"ID":               "11111111-1111-1111-1111-111111111111",
			"Name":             escapingProbe,
			"MusicBrainzID":    "esc-probe-mbid",
			"Monitored":        true,
			"AcquiredReleases": 1,
			"TotalReleases":    3,
			"LastScanLabel":    "never",
		}},
	})

	for _, want := range escapingProbeEscapedOnce {
		assert.Contains(t, body, want, "a value must reach the browser escaped once")
	}
	for _, unwanted := range escapingProbeEscapedTwice {
		assert.NotContains(t, body, unwanted,
			"%q means the value was escaped twice: it is on the page as a literal entity", unwanted)
	}

	// The attribute paths too. The artists card repeats the name in three
	// aria-labels, which is where a template-level filter and an engine-level
	// escape are easiest to end up with both of.
	assert.Contains(t, body, `aria-label="Sync discography for Tom &amp; Jerry`,
		"an escaped value must survive into an attribute, escaped once")
	assert.NotContains(t, body, "&amp;amp;",
		"no double escape anywhere in the rendered partial, text or attribute")
}

// TestPartials_MarkupInDataStaysMarkupFree is the other direction of the same
// invariant: one escape must still be enough to keep a value inert. Deleting the
// redundant filter is only safe because the surviving escape is unconditional,
// and this is the assertion that says so.
func TestPartials_MarkupInDataStaysMarkupFree(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	hostile := `<img src=x onerror="alert(1)">`
	body := renderTemplateFile(t, templatesDir, "partials/artists.html", map[string]any{
		"artists": []map[string]any{{
			"ID":               "22222222-2222-2222-2222-222222222222",
			"Name":             hostile,
			"MusicBrainzID":    "</span><script>alert(1)</script>",
			"Monitored":        true,
			"AcquiredReleases": 0,
			"TotalReleases":    0,
			"LastScanLabel":    "never",
		}},
	})

	assert.NotContains(t, body, "<img src=x",
		"data must not become markup: the engine's escape is what keeps it inert")
	assert.NotContains(t, body, "<script>alert(1)</script>",
		"data must not become markup")
	assert.NotContains(t, body, `onerror="alert(1)"`,
		"an unescaped attribute value would break out of the attribute")
	assert.Contains(t, body, "&lt;img src=x",
		"the value must still be readable where it belongs: escaped, not dropped")
	assert.NotContains(t, body, "&amp;lt;",
		"escaped once, not twice")
}

// TestTemplates_ApplyNoRedundantEscapeFilter is the guard that stops the defect
// returning, one template at a time - which is how it arrived.
//
// The ban is absolute rather than per-site because the engine's escape is
// unconditional: it applies to every string in every template, so an explicit
// `| escape` is always the second one. The only thing that could make it
// legitimate is an `{% autoescape off %}` block, and
// TestPongo2Engine_AutoescapeIsNeverDisabled below asserts there is none.
//
// `|e` is pongo2's registered alias for the same filter (filters_builtin.go:45),
// so it is redundant in exactly the same way and is banned with it.
func TestTemplates_ApplyNoRedundantEscapeFilter(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	// `\|\s*e\b` cannot collide with a filter like `endless` or `even`: the word
	// boundary requires the name to end at the `e`.
	filter := regexp.MustCompile(`\|\s*(escape|e)\b`)

	checked := 0
	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		checked++

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Comments are not rendered, so a comment may discuss the filter. Only
		// markup counts.
		markup := stripTemplateComments(string(raw))

		for i, line := range strings.Split(markup, "\n") {
			if !filter.MatchString(line) {
				continue
			}
			rel, _ := filepath.Rel(templatesDir, path)
			assert.Fail(t, "redundant escape filter in a template",
				"%s:%d applies an escape filter the engine already applies: %s\n"+
					"The engine escapes every value exactly once, so this escapes it twice and "+
					"the operator reads `&amp;amp;` instead of `&`. Delete the filter (DJI-590).",
				filepath.ToSlash(rel), i+1, strings.TrimSpace(line))
		}
		return nil
	})
	require.NoError(t, err)

	// A walk that finds nothing passes for the wrong reason.
	require.Greater(t, checked, 15,
		"expected to scan the whole template set; found only %d templates", checked)
}

// rawMarkupSites lists the templates allowed to render a value through pongo2's
// `safe` filter, which tells the engine *not* to escape that one value. It is
// empty on purpose.
//
// Deleting the redundant filters makes the engine's escape the only thing
// between a database row and the markup around it, so this is the one filter
// that can put the defect's mirror image back: an artist named
// `<img src=x onerror=...>` reaching the page as markup instead of text.
// Nothing in this app renders server-built markup through a value, so there is
// no site that needs it - and an entry here is a reviewed decision to accept
// that XSS surface, with the reason written down.
//
// The register is self-cleaning: an entry whose template no longer applies the
// filter is a failure, so it cannot rot into a blanket exemption.
var rawMarkupSites = map[string]string{}

// TestTemplates_RenderNoValueUnescaped is the other half of "exactly once":
// escaping once is only the floor while nothing opts out.
func TestTemplates_RenderNoValueUnescaped(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	// The filter position only; `\bsafe\b` would also match prose and a
	// variable that happens to be named safe.
	safeFilter := regexp.MustCompile(`\|\s*safe\b`)

	applied := map[string]bool{}
	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(templatesDir, path)
		rel = filepath.ToSlash(rel)
		if !safeFilter.MatchString(stripTemplateComments(string(raw))) {
			return nil
		}
		applied[rel] = true

		reason, registered := rawMarkupSites[rel]
		if registered {
			assert.NotEmpty(t, reason, "rawMarkupSites names %s without the reason it is excused", rel)
			return nil
		}
		assert.Fail(t, "value rendered unescaped in a template",
			"%s applies the `safe` filter, which is the one filter that can turn a database "+
				"row into markup - the mirror of DJI-590. The engine already escapes every "+
				"value exactly once, so drop it, or add it to rawMarkupSites with the reason "+
				"as a reviewed decision.", rel)
		return nil
	})
	require.NoError(t, err)

	// The register may not outlive the filter it excuses.
	for site := range rawMarkupSites {
		assert.True(t, applied[site],
			"rawMarkupSites still names %s, but that template applies no `safe` filter; "+
			"remove the entry so it cannot keep re-permitting the filter after a later edit", site)
	}
}

// TestPongo2Engine_AutoescapeIsNeverDisabled guards the foundation. Every
// assertion in this file rests on the engine escaping by default, and one call
// would remove it from the whole application at once - pongo2's autoescape is a
// package-level global, not a per-set option.
func TestPongo2Engine_AutoescapeIsNeverDisabled(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	backend := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(templatesDir))), "backend")

	// Test files are skipped because this one contains the string it looks for.
	offenders := []string{}
	scanned := 0
	err := filepath.WalkDir(backend, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if regexp.MustCompile(`SetAutoescape\s*\(\s*false\s*\)`).MatchString(stripGoComments(string(raw))) {
			rel, _ := filepath.Rel(backend, path)
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, scanned, 50, "expected to scan the backend tree; found only %d Go files", scanned)

	assert.Empty(t, offenders,
		"SetAutoescape(false) disarms escaping for every template at once, and the whole "+
			"template set now relies on it; if a template genuinely needs unescaped output, "+
			"apply `| safe` at that one site instead")
}

// renderTemplateFile renders a template from the real template directory with
// the real engine, which is the path a page takes.
func renderTemplateFile(t *testing.T, templatesDir, name string, ctx map[string]any) string {
	t.Helper()

	engine := NewPongo2(templatesDir, ".html")

	var buf bytes.Buffer
	require.NoError(t, engine.Render(&buf, name, ctx),
		"%s must render; the engine resolves it by the same name a handler uses", name)
	require.NotEmpty(t, buf.String(), "%s rendered nothing", name)
	return buf.String()
}

// stripTemplateComments removes pongo2 `{# #}` and HTML comments, so a guard
// over markup does not fail on prose about the markup.
func stripTemplateComments(template string) string {
	template = regexp.MustCompile(`(?s)\{#.*?#\}`).ReplaceAllString(template, "")
	return regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(template, "")
}

// stripGoComments removes `//` and `/* */` comments for the same reason.
func stripGoComments(src string) string {
	src = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	return regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(src, "")
}

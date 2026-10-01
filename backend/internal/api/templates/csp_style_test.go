package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The app sends `style-src 'self'`, so anything inline is refused and logged.
// Three things in this package interact with that policy, and all three used to
// break it:
//
//   - the master player carried `style="display:none"`, and the track progress
//     fill carried `style="width:0%"`. Both went into the stylesheet.
//   - htmx injects its .htmx-indicator rules as an inline <style> block at
//     DOMContentLoaded. That block is refused too, which is why "Searching…"
//     used to stay on the browse page forever, and why every page load logged a
//     CSP violation even after the two attributes were gone. It is switched off
//     through the htmx-config meta instead.
//
// Relaxing the policy to 'unsafe-inline' would make all of this disappear and
// re-open the hole the policy exists to close, so the fix is always the markup
// rather than the header. These three tests are what keeps that true.

// inlineStyleRE matches a style attribute in a template. Comments are stripped
// first: several explain the policy by quoting the attribute they replaced, and
// a quoted example must not read as a live one.
var inlineStyleRE = regexp.MustCompile(`(?i)\sstyle\s*=`)

// htmlCommentRE strips <!-- ... --> so prose about a style attribute cannot be
// read as one. Not the CSS commentRE in stylesheet_coverage_test.go, which
// matches /* ... */.
var htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// baseIndicatorRule matches the rule that hides an indicator, and only that one.
// Anchored to a line start so ".htmx-request.htmx-indicator {" cannot satisfy it.
var baseIndicatorRule = regexp.MustCompile(`(?m)^\s*\.htmx-indicator\s*\{`)

// htmxConfigRE pulls the JSON out of the htmx-config meta so it can be parsed
// rather than pattern-matched.
var htmxConfigRE = regexp.MustCompile(`<meta\s+name="htmx-config"\s+content='([^']*)'`)

// TestNoTemplateCarriesAnInlineStyleAttribute fails on the shape of the defect
// DJI-528 recorded, wherever it reappears. Reading the templates rather than
// the served HTML is deliberate: an attribute added inside a pongo2 conditional
// is only sometimes rendered, and a guard that has to guess when would miss it.
func TestNoTemplateCarriesAnInlineStyleAttribute(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	offenders := templateFilesWith(t, templatesDir, func(raw string) bool {
		return inlineStyleRE.MatchString(stripHTMLComments(raw))
	})

	assert.Empty(t, offenders,
		"these templates carry a style attribute, which `style-src 'self'` refuses: %v. "+
			"Move the rule into styles.css; do not add 'unsafe-inline'", offenders)
}

// TestHtmxDoesNotInjectIndicatorStyles pins the meta tag. htmx reads it before
// it initialises, so removing it does not degrade quietly: the injection comes
// back, the CSP refuses it again, and every page load logs a violation while the
// indicator rules the injection used to supply go unapplied.
func TestHtmxDoesNotInjectIndicatorStyles(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	raw, err := os.ReadFile(filepath.Join(templatesDir, "layouts", "base.html"))
	require.NoError(t, err)

	match := htmxConfigRE.FindStringSubmatch(stripHTMLComments(string(raw)))
	require.NotNil(t, match,
		"base.html has no <meta name=\"htmx-config\">; htmx will inject its "+
			"indicator rules as an inline <style> block and `style-src 'self'` "+
			"will refuse it on every page load")

	// Parsed, not pattern-matched. A substring check passes for a value htmx
	// cannot parse - an unclosed brace, a trailing comma, the property written
	// as the string "false" - and htmx then ignores the whole configuration and
	// goes back to injecting the block this guard exists to prevent. The
	// pointer distinguishes an absent property from an explicit false.
	var cfg struct {
		IncludeIndicatorStyles *bool `json:"includeIndicatorStyles"`
	}
	require.NoError(t, json.Unmarshal([]byte(match[1]), &cfg),
		"the htmx-config meta is not valid JSON, so htmx ignores it: %s", match[1])
	require.NotNil(t, cfg.IncludeIndicatorStyles,
		"the htmx-config meta has no includeIndicatorStyles property, so the "+
			"default (true) stands and htmx injects the style block: %s", match[1])
	assert.False(t, *cfg.IncludeIndicatorStyles,
		"the meta tag turns indicator-style injection back on: %s", match[1])
}

// TestTheStylesheetCarriesTheIndicatorRules is the coupling that makes the meta
// tag safe, and the reason this file exists rather than a one-line change.
//
// The injection is off, so these rules are the only copy that will ever apply.
// Delete them as "duplicated by htmx" and every loading indicator in the product
// is visible from first paint and never hidden again - which is exactly the bug
// DJI-527 reported, arriving through a different door.
func TestTheStylesheetCarriesTheIndicatorRules(t *testing.T) {
	_, cssPath := webAssetPaths(t)

	raw, err := os.ReadFile(cssPath)
	require.NoError(t, err)
	css := string(raw)

	// The pair, and the request state that reveals it. Both halves matter:
	// .htmx-indicator alone hides nothing without the .htmx-request rule that
	// reveals it.
	//
	// Anchored to the start of a line on purpose. A plain Contains for
	// ".htmx-indicator {" is also satisfied by ".htmx-request.htmx-indicator {",
	// because the substring matches the tail of it - which a mutation check
	// caught: renaming the base rule alone left the assertion true.
	assert.True(t, baseIndicatorRule.MatchString(css),
		"the base .htmx-indicator rule is gone; htmx no longer injects it, so "+
			"nothing hides an indicator when its request settles")
	assert.Contains(t, css, ".htmx-request .htmx-indicator",
		"nothing reveals the indicator while a request is in flight, so it would "+
			"never appear at all")
	assert.Contains(t, css, ".htmx-request.htmx-indicator",
		"an indicator that is itself the request trigger is never revealed")
}

// stripHTMLComments removes HTML comments so prose about an attribute cannot be
// mistaken for the attribute.
func stripHTMLComments(s string) string {
	return htmlCommentRE.ReplaceAllString(s, "")
}

// templateFilesWith returns the template paths, relative to the templates
// directory, whose source satisfies match.
func templateFilesWith(t *testing.T, dir string, match func(string) bool) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match(string(raw)) {
			rel, _ := filepath.Rel(dir, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	return found
}
package api

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This app serves `script-src 'self'` and nothing else. htmx compiles two
// things with eval: a values attribute whose value begins with "js:", and any
// hx-on handler. Under this policy both THROW at the moment the attribute is
// evaluated -- the browser reports it, and htmx silently issues no request.
//
// That is not hypothetical. The Add Artist picker built its candidate pick with
// a "js:" values expression, so clicking a candidate threw
//
//	Evaluating a string as JavaScript violates the following Content Security
//	Policy directive because 'unsafe-eval' is not an allowed source of script
//
// made no POST at all, and the operator got silence. Measured in a browser
// before the fix: 5 candidates rendered, the click produced zero requests, and
// /artists ended with no artist.
//
// The fix is on the template side -- plain form values collected by hx-include
// -- and this test is what stops it coming back, or stopping someone "fixing"
// the next one by loosening the header instead.

// evalValues captures the value of a quoted htmx values attribute. RE2 has no
// backreferences, so the opening and closing quote are not matched against each
// other. That is enough here: a plain JSON object such as {"role": "user"}
// captures only a fragment and so never looks like an expression, while a
// single-quoted expression captures whole.
var evalValues = regexp.MustCompile(`hx-vals\s*=\s*['"]([^'"]*)['"]`)

// evalHandler matches htmx's inline event-handler attributes, also eval'd.
var evalHandler = regexp.MustCompile(`\bhx-on(:[a-zA-Z-]+)?\s*=`)

// htmlComment is stripped before scanning. A comment is never executed, so
// prose explaining why eval is banned must not itself trip the ban.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

func TestNoTemplateDependsOnEval(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "ops", "web", "templates")

	scanned := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		rel, _ := filepath.Rel(dir, path)
		body := htmlComment.ReplaceAllString(string(raw), "")

		for _, m := range evalValues.FindAllStringSubmatch(body, -1) {
			assert.False(t, strings.HasPrefix(strings.TrimSpace(m[1]), "js:"),
				"%s: this htmx values attribute is a JS expression, which htmx compiles with "+
					"eval. The app serves script-src 'self', so it throws and no request is made. "+
					"Carry the values as inputs and collect them with hx-include instead.", rel)
		}
		for _, m := range evalHandler.FindAllString(body, -1) {
			assert.Fail(t, "htmx compiles "+m+" with eval",
				"%s: the app serves script-src 'self', so this handler never runs", rel)
		}
		return nil
	})
	require.NoError(t, err)

	assert.Greater(t, scanned, 5,
		"expected the template tree to hold several files; scanning %d means this guard has "+
			"stopped looking at the templates it was written for", scanned)
}

// scriptSrcDirectives returns every script-src directive found in a file, so the
// policy is checked where it is actually written rather than in a copy of it.
func scriptSrcDirectives(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	body := htmlComment.ReplaceAllString(string(raw), "")

	var found []string
	rest := body
	for {
		i := strings.Index(rest, "script-src")
		if i < 0 {
			return found
		}
		rest = rest[i+len("script-src"):]
		end := strings.IndexAny(rest, ";\"")
		if end < 0 {
			return append(found, rest)
		}
		found = append(found, rest[:end])
		rest = rest[end:]
	}
}

// The header must not offer eval either. This is the other half of the pair:
// the template test says no template needs it, this says we are not paying for
// it. Both main.go and the Caddyfile declare the policy, and a deployment that
// only goes through the proxy is served the Caddyfile's copy, so both are
// checked. 'unsafe-eval' is not a price worth paying for one HTML attribute.
func TestShippedCSPDoesNotOfferUnsafeEval(t *testing.T) {
	for _, rel := range []string{
		filepath.Join("..", "..", "cmd", "server", "main.go"),
		filepath.Join("..", "..", "..", "ops", "caddy", "Caddyfile"),
	} {
		directives := scriptSrcDirectives(t, rel)
		require.NotEmpty(t, directives,
			"%s declares no script-src; if the header moved, point this guard at its new home "+
				"rather than letting it pass on a file that stopped setting one", rel)
		for _, d := range directives {
			assert.NotContains(t, d, "unsafe-eval",
				"%s offers 'unsafe-eval' in script-src. Fix the template instead: a values "+
					"expression or hx-on handler that needs eval becomes plain form values.", rel)
		}
	}
}

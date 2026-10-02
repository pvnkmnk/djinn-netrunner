package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every control that posts with htmx carries `htmx-request` while its request is
// in flight, and `styles.css` reads that class as the busy state: opacity 0.7 and
// `cursor: wait !important`. htmx is supposed to remove it when the request ends.
//
// It does not, for any control that also carries `hx-disabled-elt`, and the
// reason is inside htmx 1.9.10 rather than in this app. Its indicator bookkeeping
// and its disabled-element bookkeeping share ONE counter per element -
// `htmx-internal-data.requestCount`. Marking an element in-flight increments it
// once; disabling what `hx-disabled-elt` names increments it a second time; the
// release step decrements it once per list. An element that is both is counted
// twice and released once, so the indicator half never reaches zero.
//
// Measured live on the watchlist SYNC button (DJI-571): requestCount 2 at
// afterSwap, `htmx-request` still on the element at afterRequest, and `disabled`
// correctly cleared - the one decrement that does reach zero belongs to the
// disabled loop. Remove the attribute from the same control and the indicator
// half cleans up normally (requestCount 1, class gone), which is what makes this
// a shared counter and not something about htmx targets.
//
// So the fix has to live app-wide rather than per control, and it cannot live in
// htmx. htmx fires `htmx:afterRequest` *after* its own release step, which is the
// last moment the control can be returned to idle, and that event fires for
// success, 4xx/5xx, abort and timeout alike. app.js clears the class there.
//
// The three tests below pin that fix from both sides: the clearing path must
// exist in app.js, the busy state it removes must still be declared in the
// stylesheet, and the `hx-disabled-elt` attributes that trigger the defect must
// still be there - otherwise the guard would be asserting a code path nothing in
// the product uses, and DJI-571 would be able to return through the templates
// without a single test going red.

// afterRequestListenerRE matches app.js registering the handler that clears the
// indicator. Anchored on the call rather than on the event name alone, because
// the event name also appears in the comment explaining the defect.
var afterRequestListenerRE = regexp.MustCompile(
	`addEventListener\(\s*'htmx:afterRequest'`)

// controlRE matches a form control, so the hx-disabled-elt sweep below reads
// attributes off real controls rather than off any tag that mentions them.
var controlRE = regexp.MustCompile(`(?s)<(button|input|select|textarea)\b[^>]*>`)

// disabledAttrRE pulls hx-disabled-elt's value out of a control's markup.
var disabledAttrRE = regexp.MustCompile(`hx-disabled-elt="([^"]*)"`)

// requestClassRuleRE matches the busy-state rule itself, anchored to a line start
// so `.htmx-request .htmx-indicator` cannot satisfy it.
var requestClassRuleRE = regexp.MustCompile(`(?m)^\s*\.htmx-request\s*\{`)

// appJSPath finds the console script beside the templates webAssetPaths locates.
func appJSPath(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "ops", "web", "templates")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Join(dir, "ops", "web", "static", "js", "app.js")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("could not find ops/web/static/js/app.js above the test directory")
	return ""
}

// stripJSComments removes // and /* */ comments, skipping over string literals so
// a URL inside a string is not mistaken for a comment.
//
// This is load-bearing rather than tidy: the handler this file guards explains
// the defect in a comment that names `htmx:afterRequest` and `htmx-request`. Scan
// the file with its comments intact and every assertion below is satisfied by the
// prose describing the fix, whatever the code does - the same hole
// stripHTMLComments closes for the CSP guards, in a different language.
func stripJSComments(s string) string {
	var out strings.Builder
	const (
		code = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
	)
	state := code

	for i := 0; i < len(s); i++ {
		c := s[i]
		next := byte(0)
		if i+1 < len(s) {
			next = s[i+1]
		}

		switch state {
		case code:
			switch {
			case c == '/' && next == '/':
				state = lineComment
				i++
			case c == '/' && next == '*':
				state = blockComment
				i++
			case c == '\'':
				state = singleQuote
				out.WriteByte(c)
			case c == '"':
				state = doubleQuote
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		case lineComment:
			if c == '\n' {
				state = code
				out.WriteByte(c)
			}
		case blockComment:
			if c == '*' && next == '/' {
				state = code
				i++
			}
		case singleQuote:
			out.WriteByte(c)
			if c == '\\' && next != 0 {
				i++
				out.WriteByte(next)
			} else if c == '\'' {
				state = code
			}
		case doubleQuote:
			out.WriteByte(c)
			if c == '\\' && next != 0 {
				i++
				out.WriteByte(next)
			} else if c == '"' {
				state = code
			}
		}
	}

	return out.String()
}

// jsBraceBlock returns the body that follows marker in a stripped script,
// brace-matched from the first `{` after it.
//
// Reading a specific block rather than grepping the file is what makes these
// assertions mean anything. A whole-file Contains for the clearer is satisfied
// by its function declaration, so a mutation that empties the listener body
// still passes - which a mutation check caught before this helper existed.
func jsBraceBlock(t *testing.T, src, marker string) string {
	t.Helper()

	start := strings.Index(src, marker)
	require.NotEqual(t, -1, start, "%s is gone from app.js", marker)

	open := strings.Index(src[start:], "{")
	require.NotEqual(t, -1, open, "%s has no body", marker)

	depth, i := 0, start+open
	for ; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start+open+1 : i]
			}
		}
	}

	t.Fatalf("unbalanced braces after %s", marker)
	return ""
}

// TestTheRequestIndicatorIsClearedWhenTheRequestEnds is the guard for DJI-571.
// It asserts the whole clearing path, not the presence of the event name: the
// handler must be registered on htmx:afterRequest, that handler must call the
// clearing routine, and the routine must remove htmx's in-flight class - both
// spellings of it, since extensions add htmx-request-* members to the same class
// list.
func TestTheRequestIndicatorIsClearedWhenTheRequestEnds(t *testing.T) {
	raw, err := os.ReadFile(appJSPath(t))
	require.NoError(t, err, "app.js must exist")

	app := stripJSComments(string(raw))

	listeners := afterRequestListenerRE.FindAllString(app, -1)
	require.Len(t, listeners, 1,
		"app.js must register exactly one htmx:afterRequest listener; found %d. "+
			"htmx fires that event *after* its own release step, so it is the last "+
			"moment a control can be returned to idle - and it is the only point "+
			"that covers success, error, abort and timeout alike", len(listeners))

	// The listener's own body, not the file. Asserting that the file mentions
	// the clearer is satisfied by the function being *declared*, so a listener
	// that no longer calls it passes - the shape of mutation M3, which this
	// pair of assertions now catches.
	handler := jsBraceBlock(t, app, "addEventListener('htmx:afterRequest'")
	assert.Contains(t, handler, "clearRequestIndicator",
		"the htmx:afterRequest listener does not call the clearing routine, so "+
			"nothing returns the control to idle when the request ends (DJI-571)")
	assert.Contains(t, handler, ".elt",
		"the htmx:afterRequest listener never reads detail.elt, so it cannot know "+
			"which element issued the request and clears the wrong one (DJI-571)")

	body := jsBraceBlock(t, app, "function clearRequestIndicator(")

	assert.Contains(t, body, "'htmx-request'",
		"the clearing routine never names htmx's own in-flight class, so it "+
			"cannot be the thing that removes it (DJI-571)")
	assert.Contains(t, body, "htmx-request-",
		"the clearing routine does not cover the htmx-request-* family, so an "+
			"extension's indicator class would outlive its request (DJI-571)")
	assert.Contains(t, body, "classList.remove",
		"the clearing routine never removes anything; a filter that only reads "+
			"the class list would satisfy the two assertions above (DJI-571)")
}

// TestTheBusyStateIsStillDeclaredInTheStylesheet keeps the fix honest. Clearing
// the class is only the right answer while the class means something: delete the
// rule and every posting control looks permanently idle, which is the same
// dishonesty from the other direction - it is what "just stop showing the dimmed
// state" looks like, and it is not a fix.
func TestTheBusyStateIsStillDeclaredInTheStylesheet(t *testing.T) {
	_, cssPath := webAssetPaths(t)

	raw, err := os.ReadFile(cssPath)
	require.NoError(t, err)
	css := string(raw)

	require.True(t, requestClassRuleRE.MatchString(css),
		"the .htmx-request rule is gone, so nothing marks a control as busy any "+
			"more; DJI-571 was fixed by returning the control to idle, not by "+
			"removing the signal that says it is working")

	rule := css[requestClassRuleRE.FindStringIndex(css)[0]:]
	rule = rule[:strings.Index(rule, "}")]

	assert.Contains(t, rule, "opacity",
		"the .htmx-request rule no longer dims the control, so an in-flight "+
			"request is invisible")
	assert.Contains(t, rule, "cursor",
		"the .htmx-request rule no longer changes the cursor, so a request that "+
			"takes a moment reads as an unresponsive control")
}

// TestTheControlsThatTriggerTheSharedCounterStillCarryIt keeps this file from
// passing on nothing. The defect needs a control that is both the indicator and
// its own disabled element, so if every hx-disabled-elt went away the mechanism
// would be dormant - and DJI-571 would come straight back the moment one
// returned, with these three tests still green.
func TestTheControlsThatTriggerTheSharedCounterStillCarryIt(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	var carriers []string
	nonForm := []string{}

	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(templatesDir, path)
		rel = filepath.ToSlash(rel)

		for _, control := range controlRE.FindAllString(stripHTMLComments(string(raw)), -1) {
			if !strings.Contains(control, "hx-disabled-elt") {
				continue
			}
			tag := controlRE.FindStringSubmatch(control)[1]
			carriers = append(carriers, rel+" <"+tag+">")
			if tag != "button" && tag != "input" && tag != "select" && tag != "textarea" {
				nonForm = append(nonForm, rel+" <"+tag+">")
			}
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(carriers)
	assert.NotEmpty(t, carriers,
		"no control carries hx-disabled-elt any more, so the shared-counter "+
			"defect this file guards has nothing left to trigger it - and the "+
			"clearing handler is now guarding a path nothing uses")
	assert.Empty(t, nonForm,
		"hx-disabled-elt on a tag that has no disabled state does nothing, so "+
			"that control gets neither the double-submit guard nor a pending "+
			"state: %v", nonForm)

	t.Logf("hx-disabled-elt carried by %d control(s): %v", len(carriers), carriers)
}

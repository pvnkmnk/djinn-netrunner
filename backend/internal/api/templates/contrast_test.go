package templates

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file guards the contrast of the stylesheet's text tokens against the
// surfaces those tokens sit on.
//
// PRODUCT.md records WCAG 2.1 AA as "the floor, confirmed by the user, not an
// aspiration", and a token is exactly the kind of value a floor applies to:
// --text-muted shipped as #484f58, which is 2.3:1 on --bg-primary. Two
// separate comments in styles.css recorded that it failed AA and stepped around
// it - the picker's detail text chose --text-secondary for that reason - so the
// value was known to be wrong and nothing failed. A comment cannot fail a
// build. This can.
//
// The ratios are recomputed from ops/web/static/css/styles.css rather than
// restated here, so editing the token block is what moves them. Nothing else in
// this repo reads the :root block: the class-coverage guard next door looks at
// selectors, the CSP test looks at inline styles, and Playwright asserts
// behaviour, which a 2.3:1 footer passes.

// minContrastAA is the WCAG 2.1 AA ratio for body text (1.4.3).
const minContrastAA = 4.5

// contrastPairs are the (text token, background token) pairings that must clear
// AA. Every declared text token is paired with every declared background token
// except the ones named in contrastExceptions below, and a token that is not
// covered either way fails TestStylesheet_EveryTextTokenIsPairedWithEverySurface
// - so introducing a new background cannot quietly go unchecked.
var contrastPairs = []struct{ text, bg string }{
	{"--text-primary", "--bg-primary"},
	{"--text-primary", "--bg-secondary"},
	{"--text-primary", "--bg-tertiary"},
	{"--text-primary", "--bg-card"},

	{"--text-secondary", "--bg-primary"},
	{"--text-secondary", "--bg-secondary"},
	{"--text-secondary", "--bg-tertiary"},
	{"--text-secondary", "--bg-card"},

	{"--text-muted", "--bg-primary"},
	{"--text-muted", "--bg-secondary"},
	{"--text-muted", "--bg-card"},
}

// contrastExceptions names the pairings that are deliberately not required to
// clear AA. Each entry asserts two things at once, both checked below: the pair
// really is below AA today (so the entry cannot rot into a blanket exemption
// once the token is brightened), and no rule in the stylesheet puts those two
// tokens together (so "we documented it" is not standing in for "we avoid it").
var contrastExceptions = map[string]string{
	"--text-muted/--bg-tertiary": "the lightest surface: --bg-tertiary is the " +
		"hover/active wash, --text-muted reaches 4.1:1 on it, and the two are " +
		"kept apart - TestStylesheet_ExceptedPairingsNeverCoOccur fails if a " +
		"rule ever sets muted text on that surface",
}

// TestStylesheet_TextTokensClearWCAAContrast is the floor itself.
func TestStylesheet_TextTokensClearWCAAContrast(t *testing.T) {
	_, cssPath := webAssetPaths(t)
	tokens := stylesheetColorTokens(t, cssPath)

	for _, pair := range contrastPairs {
		textLum, bgLum := requireToken(t, tokens, pair.text), requireToken(t, tokens, pair.bg)
		ratio := contrastRatio(textLum, bgLum)

		assert.GreaterOrEqual(t, ratio, minContrastAA,
			"%s on %s is %.2f:1, under the %.1f:1 AA floor PRODUCT.md confirms. "+
				"Darken the background, brighten the token, or - if the pairing is "+
				"genuinely unreadable-by-design - add it to contrastExceptions with the "+
				"reason and the guard that keeps the two apart.",
			pair.text, pair.bg, ratio, minContrastAA)
	}
}

// TestStylesheet_ExceptedPairingsNeverCoOccur is the other half of an exception:
// a pairing that is allowed to be under AA is only allowed while nothing
// actually renders it.
func TestStylesheet_ExceptedPairingsNeverCoOccur(t *testing.T) {
	_, cssPath := webAssetPaths(t)
	tokens := stylesheetColorTokens(t, cssPath)

	for key := range contrastExceptions {
		text, bg, ok := strings.Cut(key, "/")
		require.True(t, ok, "exception key %q must be <text-token>/<background-token>", key)

		// The exemption is only honest while the pair is genuinely below AA.
		ratio := contrastRatio(requireToken(t, tokens, text), requireToken(t, tokens, bg))
		assert.Less(t, ratio, minContrastAA,
			"%s on %s now measures %.2f:1 and clears AA; remove the exception so the "+
				"pairing is asserted like every other one", text, bg, ratio)

		// And only honest while no rule uses the two together.
		for _, block := range stylesheetRuleBlocks(t, cssPath) {
			if !block.usesColorToken(text) || !block.usesBackgroundToken(bg) {
				continue
			}
			assert.Fail(t, fmt.Sprintf(
				"%s sets %s text over %s, a pairing registered as below the AA floor.\n"+
					"selector: %s\n"+
					"Brighten --text-muted for that surface, pick a token that clears "+
					"%.1f:1 there, or move the rule to a background that does not.",
				block.selector, text, bg, block.selector, minContrastAA))
		}
	}
}

// TestStylesheet_EveryTextTokenIsPairedWithEverySurface keeps both registers
// honest: a token in neither table is a token nothing checks, which is how
// --text-muted came to be documented-but-unenforced in the first place.
func TestStylesheet_EveryTextTokenIsPairedWithEverySurface(t *testing.T) {
	_, cssPath := webAssetPaths(t)
	tokens := stylesheetColorTokens(t, cssPath)

	var texts, backgrounds []string
	for name := range tokens {
		switch {
		case strings.HasPrefix(name, "--text-"):
			texts = append(texts, name)
		case strings.HasPrefix(name, "--bg-"):
			backgrounds = append(backgrounds, name)
		}
	}
	require.NotEmpty(t, texts, "no --text-* tokens found in the stylesheet's :root block")
	require.NotEmpty(t, backgrounds, "no --bg-* tokens found in the stylesheet's :root block")

	paired := map[string]bool{}
	for _, pair := range contrastPairs {
		paired[pair.text+"/"+pair.bg] = true
	}

	for _, text := range texts {
		for _, bg := range backgrounds {
			key := text + "/" + bg
			if paired[key] {
				continue
			}
			_, excepted := contrastExceptions[key]
			assert.True(t, excepted,
				"%s is paired with neither a contrast assertion nor an exception. Add it "+
					"to contrastPairs if it clears %.1f:1 on %s, or to contrastExceptions "+
					"with the reason it is kept off that surface.", text, minContrastAA, bg)
		}
	}

	// A pair naming a token that no longer exists is drift in the other
	// direction: the assertion silently measures nothing.
	for _, pair := range contrastPairs {
		requireToken(t, tokens, pair.text)
		requireToken(t, tokens, pair.bg)
	}
}

// TestStylesheet_TextTokensKeepTheirOrder walks the hierarchy the tokens exist
// to express. Raising a dim token until it passes AA is the right fix only
// while it stays the dim one; flattening all three to one grey would satisfy
// the floor and delete the design, and PRODUCT.md binds the look as well as the
// floor.
func TestStylesheet_TextTokensKeepTheirOrder(t *testing.T) {
	_, cssPath := webAssetPaths(t)
	tokens := stylesheetColorTokens(t, cssPath)

	primary := requireToken(t, tokens, "--text-primary")
	secondary := requireToken(t, tokens, "--text-secondary")
	muted := requireToken(t, tokens, "--text-muted")

	assert.Greater(t, primary, secondary, "--text-primary must out-shine --text-secondary")
	assert.Greater(t, secondary, muted, "--text-muted must stay the dimmest text token")

	// Distinctness with a margin, so "make them all pass" cannot be answered by
	// making them all equal. 1.15 is the smallest step that still reads as two
	// weights rather than one at these luminances.
	assert.GreaterOrEqual(t, (secondary+0.05)/(muted+0.05), 1.15,
		"--text-muted sits too close to --text-secondary to read as a separate weight")
	assert.GreaterOrEqual(t, (primary+0.05)/(secondary+0.05), 1.15,
		"--text-secondary sits too close to --text-primary to read as a separate weight")
}

// requireToken looks a token up, failing rather than returning a zero the
// caller would then compare against.
func requireToken(t *testing.T, tokens map[string]float64, name string) float64 {
	t.Helper()
	lum, ok := tokens[name]
	require.True(t, ok, "token %s is not declared in the stylesheet's :root block", name)
	return lum
}

// stylesheetColorTokens returns the relative luminance of every colour token
// declared in the stylesheet's :root block, keyed by token name.
func stylesheetColorTokens(t *testing.T, cssPath string) map[string]float64 {
	t.Helper()

	raw, err := os.ReadFile(cssPath)
	require.NoError(t, err)
	css := stripCSSComments(string(raw))

	root := regexp.MustCompile(`(?s):root\s*\{(.*?)\}`).FindStringSubmatch(css)
	require.NotNil(t, root, "no :root block in %s", cssPath)

	decl := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:\s*(#[0-9a-fA-F]{3,8})\s*;`)
	tokens := map[string]float64{}
	for _, m := range decl.FindAllStringSubmatch(root[1], -1) {
		tokens[m[1]] = hexLuminance(t, m[2])
	}
	require.NotEmpty(t, tokens, "no colour tokens parsed from the :root block in %s", cssPath)
	return tokens
}

// ruleBlock is one selector and the declarations that follow it.
type ruleBlock struct {
	selector string
	body     string
}

// The `(?:^|[^-\w])` prefix is load-bearing: a bare `border-color: var(...)`
// contains `color: var(...)` as a substring, and reading a border as a text
// colour would report a pairing that is not on the page.
func (b ruleBlock) usesColorToken(token string) bool {
	pattern := `(?:^|[^-\w])color\s*:\s*var\(` + regexp.QuoteMeta(token) + `\)`
	return regexp.MustCompile(pattern).MatchString(b.body)
}

func (b ruleBlock) usesBackgroundToken(token string) bool {
	pattern := `(?:^|[^-\w])background(?:-color)?\s*:\s*var\(` + regexp.QuoteMeta(token) + `\)`
	return regexp.MustCompile(pattern).MatchString(b.body)
}

// stylesheetRuleBlocks returns every innermost `selector { ... }` in the
// stylesheet.
//
// Matching blocks that contain no braces at all is what makes this safe against
// the @media wrappers: an at-rule's block encloses other rules, so the regex
// cannot consume it as a rule and instead yields the rules inside it, each with
// its own selector. The question asked of a block is only whether it sets a
// text token and a background token together.
func stylesheetRuleBlocks(t *testing.T, cssPath string) []ruleBlock {
	t.Helper()

	raw, err := os.ReadFile(cssPath)
	require.NoError(t, err)
	css := stripCSSComments(string(raw))

	matches := regexp.MustCompile(`(?s)([^{}]*)\{([^{}]*)\}`).FindAllStringSubmatch(css, -1)
	blocks := make([]ruleBlock, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, ruleBlock{
			selector: strings.TrimSpace(m[1]),
			body:     m[2],
		})
	}

	require.NotEmpty(t, blocks, "no rule blocks parsed from %s", cssPath)
	return blocks
}

func stripCSSComments(css string) string {
	return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
}

// hexLuminance converts #rgb / #rrggbb (and #rrggbbaa, whose alpha is refused
// rather than ignored: contrast against a translucent colour is not a number
// this test can compute) to a WCAG relative luminance.
func hexLuminance(t *testing.T, hex string) float64 {
	t.Helper()

	digits := strings.TrimPrefix(hex, "#")
	switch len(digits) {
	case 3:
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	case 8:
		require.FailNow(t, "cannot measure contrast of a translucent colour", "%s", hex)
	}
	require.Len(t, digits, 6, "unparseable colour %s", hex)

	channels := make([]float64, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(digits[i*2:i*2+2], 16, 8)
		require.NoError(t, err, "unparseable colour %s", hex)
		channels[i] = linearize(float64(v) / 255)
	}

	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

// linearize is the WCAG 2.1 sRGB transfer function.
func linearize(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// contrastRatio is WCAG 2.1's (L1 + 0.05) / (L2 + 0.05), order-independent.
func contrastRatio(a, b float64) float64 {
	hi, lo := math.Max(a, b), math.Min(a, b)
	return (hi + 0.05) / (lo + 0.05)
}

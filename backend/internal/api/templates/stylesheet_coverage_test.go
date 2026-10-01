package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file guards the relationship between ops/web/templates and
// ops/web/static/css/styles.css.
//
// Commit 3734d90 ("fix: add CRUD functionality to watchlists page") replaced
// the stylesheet's component block instead of editing it and removed 331
// lines: buttons, modals, forms, tables, the console region and the htmx
// indicator. Nothing noticed, because nothing in CI, in the validation script
// or in the e2e suite looked at templates and the stylesheet together — the
// Playwright specs assert behaviour, and a button rendering as a raw native
// <button> still passes every behavioural assertion. Fourteen classes used by
// templates lost their only rule and stayed that way.
//
// What counts as "has a rule" is deliberately stricter than "the class name
// appears somewhere in the file", because that weaker test is what let the
// deletion through. See declaredClasses below.

// intentionallyUnstyled is the reviewed register of template classes that
// legitimately have no stylesheet rule yet.
//
// These are all markup that shipped without styling — not classes the product
// decided to leave alone. Each entry therefore also names the slice that owns
// it. Adding an entry is a reviewed decision, not a way to silence the guard:
// if you add a class here you are asserting it is fine for it to be invisible,
// and you own getting it styled.
//
// Three ways this register is kept honest, all of them test failures:
//
//   - an entry that gains a rule must be removed (it would otherwise keep
//     re-permitting the class after a later edit takes the rule away again);
//   - an entry no template uses must be removed (the register has drifted);
//   - a class with no entry and no rule fails the build.
//
// The way to resolve a genuine failure is to write the rule. Only add an entry
// when the class is meant to be unstyled.
var intentionallyUnstyled = map[string]string{
	// Now-playing player in the track detail modal. Shipped unstyled.
	"np-current":       "now-playing player markup, never styled (P-DJI-28)",
	"np-info":          "now-playing player markup, never styled (P-DJI-28)",
	"np-controls":      "now-playing player markup, never styled (P-DJI-28)",
	"np-buttons":       "now-playing player markup, never styled (P-DJI-28)",
	"np-play-btn":      "now-playing player markup, never styled (P-DJI-28)",
	"np-progress":      "now-playing player markup, never styled (P-DJI-28)",
	"np-progress-bar":  "now-playing player markup, never styled (P-DJI-28)",
	"np-progress-fill": "now-playing player markup, never styled (P-DJI-28)",
	"np-total":         "now-playing player markup, never styled (P-DJI-28)",
	"np-track-title":   "now-playing player markup, never styled (P-DJI-28)",
	"np-track-artist":  "now-playing player markup, never styled (P-DJI-28)",

	// Admin surface. Unreachable until DJI-542 lands, so never styled.
	"admin-dashboard": "admin surface, unreachable until DJI-542 lands",
	"admin-nav":       "admin surface, unreachable until DJI-542 lands",
	"admin-nav-link":  "admin surface, unreachable until DJI-542 lands",
	"admin-content":   "admin surface, unreachable until DJI-542 lands",
	"admin-table":     "admin surface, unreachable until DJI-542 lands",
	"role-badge":      "admin surface, unreachable until DJI-542 lands",

	// Library browse table.
	"track-row":   "library browse table row, never styled (P-DJI-28)",
	"track-count": "library browse table cell, never styled (P-DJI-28)",
	"cell-title":  "library browse table cell, never styled (P-DJI-28)",

	// Watchlist preview list.
	"track-info":   "watchlist preview row, never styled (P-DJI-28)",
	"track-title":  "watchlist preview row, never styled (P-DJI-28)",
	"track-artist": "watchlist preview row, never styled (P-DJI-28)",
	"track-album":  "watchlist preview row, never styled (P-DJI-28)",

	// Playlists.
	"playlist-card":    "playlists card, never styled (P-DJI-28)",
	"playlist-info":    "playlists card, never styled (P-DJI-28)",
	"playlists-region": "playlists region wrapper, never styled (P-DJI-28)",
	"meta":             "playlists meta line, never styled (P-DJI-28)",

	// htmx region wrappers and page furniture.
	"artists-region":   "htmx region wrapper, never styled (P-DJI-28)",
	"libraries-region": "htmx region wrapper, never styled (P-DJI-28)",
	"profiles-region":  "htmx region wrapper, never styled (P-DJI-28)",
	"schedules-region": "htmx region wrapper, never styled (P-DJI-28)",
	"page-header":      "page heading block, never styled (DJI-525 removes these)",
	"error-banner":     "error banner, never styled (P-DJI-28)",
	"job-error-detail": "job error detail, never styled (P-DJI-28)",

	// Button variants the templates use but no rule was ever written for.
	"btn-secondary": "button variant, never styled (P-DJI-28)",
	"btn-outline":   "button variant, never styled (P-DJI-28)",
	"btn-warning":   "button variant, never styled (P-DJI-28)",

	// Dashboard console and page wrappers.
	"console-socket": "console websocket wrapper, never styled (P-DJI-28)",
	"dashboard":      "dashboard page wrapper, never styled (P-DJI-28)",
}

var (
	// classRE matches a class token in a selector.
	classRE = regexp.MustCompile(`\.([A-Za-z_][\w-]*)`)
	// qualifierRE matches a character that qualifies the class token before it,
	// turning a declaration into a state-specific mention: .btn:hover,
	// .btn::before, .btn[disabled], .btn:not(.x).
	qualifierRE = regexp.MustCompile(`^[A-Za-z_:\-\[(]`)
	// compoundSplitRE splits a selector into compound selectors.
	compoundSplitRE = regexp.MustCompile(`\s*[>+~]\s*|\s+`)
	// commentRE strips CSS comments.
	commentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// templateTagRE matches a Pongo2 expression or statement. These are removed
	// before class attributes are matched, because they can contain the quote
	// character that would otherwise terminate the attribute match early.
	templateTagRE = regexp.MustCompile(`(?s)\{%.*?%\}|\{\{.*?\}\}`)
	// classAttrRE matches a class attribute in template markup.
	classAttrRE = regexp.MustCompile(`class="([^"]*)"`)
	// classNameRE matches one whole class name.
	classNameRE = regexp.MustCompile(`^[A-Za-z_][\w-]*$`)
)

// TestTemplateClassesHaveStylesheetRules is the guard. Every class a template
// puts in a class attribute must have a declaration in the stylesheet, or be
// on the reviewed register above.
func TestTemplateClassesHaveStylesheetRules(t *testing.T) {
	templatesDir, cssPath := webAssetPaths(t)

	used := templateClasses(t, templatesDir)
	require.NotEmpty(t, used, "no template classes found — the scan itself is broken")

	css, err := os.ReadFile(cssPath)
	require.NoError(t, err)
	declared := declaredClasses(string(css))

	var undeclared []string
	for class := range used {
		if !declared[class] {
			undeclared = append(undeclared, class)
		}
	}
	sort.Strings(undeclared)

	var unregistered []string
	for _, class := range undeclared {
		if _, ok := intentionallyUnstyled[class]; !ok {
			unregistered = append(unregistered, class)
		}
	}

	if len(unregistered) > 0 {
		var b strings.Builder
		b.WriteString("these classes are used by templates but have no stylesheet rule:\n")
		for _, class := range unregistered {
			fmt.Fprintf(&b, "\n  .%s\n", class)
			fmt.Fprintf(&b, "      used by: %s\n", strings.Join(used[class], ", "))
		}
		b.WriteString("\nGive each one a rule. If a class is genuinely meant to be\n")
		b.WriteString("unstyled, add it to intentionallyUnstyled with a reason in\n")
		b.WriteString("stylesheet_coverage_test.go — that is a reviewed decision,\n")
		b.WriteString("not a way to make this test pass.\n")
		t.Error(b.String())
	}

	// An entry that now has a rule must be removed. Left in place it would keep
	// exempting the class, so a later edit could take the rule away again and
	// this test would still be green — which is the failure it exists to catch.
	var stale []string
	for class := range intentionallyUnstyled {
		if declared[class] {
			stale = append(stale, class)
		}
	}
	sort.Strings(stale)
	assert.Empty(t, stale,
		"these entries in intentionallyUnstyled now have a stylesheet rule — remove them, "+
			"or the register will keep re-permitting them if the rule is later deleted")

	// An entry no template uses any more has drifted out of the register.
	var orphaned []string
	for class := range intentionallyUnstyled {
		if _, ok := used[class]; !ok {
			orphaned = append(orphaned, class)
		}
	}
	sort.Strings(orphaned)
	assert.Empty(t, orphaned,
		"these entries in intentionallyUnstyled are no longer used by any template — remove them")
}

// TestStylesheetBracesBalance catches the mechanical damage 3734d90 did
// alongside the deletion: the file it left behind had an unterminated
// `.watchlist-card {`, so every rule after it parsed as one selector list and
// silently stopped applying.
func TestStylesheetBracesBalance(t *testing.T) {
	_, cssPath := webAssetPaths(t)

	css, err := os.ReadFile(cssPath)
	require.NoError(t, err)

	stripped := commentRE.ReplaceAllString(string(css), "")
	open := strings.Count(stripped, "{")
	close := strings.Count(stripped, "}")
	assert.Equal(t, open, close,
		"unbalanced braces in the stylesheet: %d open, %d close — rules after the "+
			"unterminated block will not apply", open, close)
}

// webAssetPaths locates ops/web from the test's working directory by walking up
// until the template directory turns up, so the test does not hard-code how
// deep it sits in the tree.
func webAssetPaths(t *testing.T) (templatesDir, cssPath string) {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "ops", "web", "templates")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, filepath.Join(dir, "ops", "web", "static", "css", "styles.css")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("could not find ops/web/templates above %s", dir)
	return "", ""
}

// templateClasses returns every class name a template puts in a class
// attribute, mapped to the templates that use it.
//
// The order of the two steps matters. Template expressions are stripped before
// the class attribute is matched, because templates put quoted strings inside
// template tags inside a double-quoted attribute:
//
//	class="job-card {% if job.State == "failed" %}job-card-failed{% endif %}"
//
// Matching the attribute first stops at the inner quote and yields junk tokens
// such as "if", which then look like a missing rule.
//
// A token ending in "-" is dropped: it is the residue of a name the template
// composes at render time (badge-{{ .Source }} leaves "badge-"), not a class in
// its own right. Values produced by such an expression are dynamic and cannot
// be checked against the stylesheet at build time.
func templateClasses(t *testing.T, dir string) map[string][]string {
	t.Helper()

	used := map[string]map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body := templateTagRE.ReplaceAllString(string(raw), " ")
		for _, m := range classAttrRE.FindAllStringSubmatch(body, -1) {
			for _, token := range strings.Fields(m[1]) {
				if strings.HasSuffix(token, "-") || !classNameRE.MatchString(token) {
					continue
				}
				if used[token] == nil {
					used[token] = map[string]bool{}
				}
				used[token][filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	require.NoError(t, err)

	out := map[string][]string{}
	for class, files := range used {
		list := make([]string, 0, len(files))
		for f := range files {
			list = append(list, f)
		}
		sort.Strings(list)
		out[class] = list
	}
	return out
}

// declaredClasses returns the classes that carry at least one unconditional
// declaration in the stylesheet.
//
// Three things deliberately do not count as a declaration:
//
//   - a qualified mention. `.btn:hover` styles .btn on hover, not .btn.
//     Counting mentions is exactly what hid the 3734d90 deletion: afterwards
//     the only surviving `.btn` in the file was inside a :focus-visible list,
//     so a "does this class appear in the stylesheet" check said yes while the
//     class had no appearance at all.
//   - anything inside @media (prefers-reduced-motion). That block only disables
//     animation; it cannot establish how something looks.
//   - @keyframes bodies, whose from/to steps are not selectors.
func declaredClasses(css string) map[string]bool {
	stripped := commentRE.ReplaceAllString(css, "")

	declared := map[string]bool{}
	// One entry per open at-rule; true marks a reduced-motion media query.
	var stack []bool
	var buf strings.Builder

	for i := 0; i < len(stripped); {
		switch stripped[i] {
		case '{':
			selector := strings.TrimSpace(buf.String())
			buf.Reset()

			switch {
			case strings.HasPrefix(selector, "@keyframes"), strings.HasPrefix(selector, "@-webkit-keyframes"):
				i = skipBlock(stripped, i)
				continue
			case strings.HasPrefix(selector, "@media"):
				stack = append(stack, strings.Contains(selector, "prefers-reduced-motion"))
			default:
				inReducedMotion := false
				for _, flag := range stack {
					inReducedMotion = inReducedMotion || flag
				}
				stack = append(stack, false)
				if selector != "" && !inReducedMotion {
					addCompoundClasses(selector, declared)
				}
			}
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			buf.Reset()
		case ';':
			// Ends a statement at-rule such as @import.
			if len(stack) == 0 {
				buf.Reset()
			}
		default:
			buf.WriteByte(stripped[i])
		}
		i++
	}
	return declared
}

// addCompoundClasses records the classes a selector declares. A class counts
// only when nothing qualifies it, so `.btn.block` declares both but `.btn:hover`
// declares neither — it is a state, not a declaration of .btn.
func addCompoundClasses(selector string, into map[string]bool) {
	for _, compound := range compoundSplitRE.Split(selector, -1) {
		compound = strings.TrimSpace(compound)
		if compound == "" {
			continue
		}
		for _, loc := range classRE.FindAllStringSubmatchIndex(compound, -1) {
			if loc[1] < len(compound) && qualifierRE.MatchString(compound[loc[1]:]) {
				continue
			}
			into[compound[loc[2]:loc[3]]] = true
		}
	}
}

// skipBlock returns the index just past the block opened at openIdx.
func skipBlock(css string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(css)
}
